// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// When a user creates an account using an invitation, we track the new and old
// user and, for potential analysis/troubleshooting purposes, the original token

package invited

import (
	"context"
	"fmt"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"

	"xorm.io/builder"
)

func init() {
	db.RegisterModel(new(Invitation))
	db.RegisterModel(new(Using))
}

// token tracking goes into a separate table, because it is only needed for
// analysis and would pollute the cache for core functionality (checking the
// number of invites since time etc).
//
// this table can also be garbage collected freely, the de-normalized created
// timestamp is to simplify that
type Using struct {
	ID           int64              `xorm:"pk autoincr"`
	InvitationID int64              `xorm:"index references(invitation, id)"`
	Token        string             `xorm:"TEXT"`
	UsedUnix     timeutil.TimeStamp `xorm:"used index"`
}

// indices are motivated by the queries which we need
// - entries by inviter within time: inviter_when
// - invitations total since: when
// - track tree upwards: invitee
//
// deliberately not using references (foreign key) to keep the invitation tree
// intact when users get deleted
type Invitation struct {
	ID        int64              `xorm:"pk autoincr"`
	InviterID int64              `xorm:"inviter index(inviter_used)"`
	InviteeID int64              `xorm:"invitee index unique"`
	UsedUnix  timeutil.TimeStamp `xorm:"used index(inviter_used) index"`
}

func Log(ctx context.Context, inviterID, inviteeID int64, token string) error {
	// Would be nice if we could have CHECK constraints, but I guess that would
	// break db portability? At least xorm should not implement them directly
	if inviterID == inviteeID {
		return fmt.Errorf("A user cannot invite themselves (id %d)", inviterID)
	}
	loop, err := db.GetEngine(ctx).
		Table(&Invitation{}).
		Where(builder.Eq{"inviter": inviteeID}).
		Exist()
	if err != nil {
		return err
	}
	if loop {
		return fmt.Errorf("Invitee (id %d) can not be an inviter(id %d) (loop)", inviteeID, inviterID)
	}

	when := timeutil.TimeStamp(time.Now().Unix())
	invitation := &Invitation{
		InviterID: inviterID,
		InviteeID: inviteeID,
		UsedUnix:  when,
	}
	invitationID, err := db.GetEngine(ctx).Insert(invitation)
	if err != nil {
		return err
	}
	using := &Using{
		InvitationID: invitationID,
		Token:        token,
		UsedUnix:     when,
	}
	if _, err := db.GetEngine(ctx).Insert(using); err != nil {
		return err
	}
	return nil
}

func Since(ctx context.Context, inviterID, seconds int64) (int64, error) {
	since := timeutil.TimeStampNow().Add(-seconds)
	count, err := db.GetEngine(ctx).
		Table(&Invitation{}).
		Where(builder.Eq{"inviter": inviterID}).
		And(builder.Gt{"used": since}).
		Count()
	return count, err
}
