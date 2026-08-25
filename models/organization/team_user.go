// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package organization

import (
	"context"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/timeutil"

	"xorm.io/builder"
)

// MembershipReason defines why a user is member of a team/org
type MembershipReason int

const (
	// MembershipReasonUnknown represents a membership created before Forgejo started to track membership provenance
	MembershipReasonUnknown MembershipReason = iota // Represented as zero to match the default values created by the SQL migration

	// MembershipReasonOrgFounder represents a membership that was induced by creating the organization
	MembershipReasonOrgFounder // 1

	// MembershipReasonByUser represents a membership created by another user (already member of the organization or site admin)
	MembershipReasonByUser // 2

	// MembershipReasonByAuthProvider represents a membership created by an authentication source whose metadata was mapped to team membership
	MembershipReasonByAuthProvider // 3
)

// TeamUser represents an team-user relation.
type TeamUser struct {
	ID                     int64                               `xorm:"pk autoincr"`
	OrgID                  int64                               `xorm:"INDEX"`
	TeamID                 int64                               `xorm:"UNIQUE(s)"`
	UID                    int64                               `xorm:"UNIQUE(s)"`
	CreatedUnix            optional.Option[timeutil.TimeStamp] `xorm:"created_unix"`
	Reason                 MembershipReason
	CreatedByUserID        optional.Option[int64] `xorm:"index REFERENCES(user, id)"`
	CreatedByLoginSourceID optional.Option[int64] `xorm:"INDEX REFERENCES(login_source, id)"`
}

// IsTeamMember returns true if given user is a member of team.
func IsTeamMember(ctx context.Context, orgID, teamID, userID int64) (bool, error) {
	return db.GetEngine(ctx).
		Where("org_id=?", orgID).
		And("team_id=?", teamID).
		And("uid=?", userID).
		Table("team_user").
		Exist()
}

// GetTeamUsersByTeamID returns team users for a team
func GetTeamUsersByTeamID(ctx context.Context, teamID int64) ([]*TeamUser, error) {
	teamUsers := make([]*TeamUser, 0, 10)
	return teamUsers, db.GetEngine(ctx).
		Where("team_id=?", teamID).
		Find(&teamUsers)
}

// SearchMembersOptions holds the search options
type SearchMembersOptions struct {
	db.ListOptions
	TeamID int64
}

// GetTeamMembers returns all members in given team of organization.
func GetTeamMembers(ctx context.Context, opts *SearchMembersOptions) ([]*user_model.User, error) {
	var members []*user_model.User
	sess := db.GetEngine(ctx)
	if opts.TeamID > 0 {
		sess = sess.In("id",
			builder.Select("uid").
				From("team_user").
				Where(builder.Eq{"team_id": opts.TeamID}),
		)
	}
	if opts.PageSize > 0 && opts.Page > 0 {
		sess = sess.Limit(opts.PageSize, (opts.Page-1)*opts.PageSize)
	}
	if err := sess.OrderBy("full_name, name").Find(&members); err != nil {
		return nil, err
	}
	return members, nil
}

// IsUserInTeams returns if a user in some teams
func IsUserInTeams(ctx context.Context, userID int64, teamIDs []int64) (bool, error) {
	return db.GetEngine(ctx).Where("uid=?", userID).In("team_id", teamIDs).Exist(new(TeamUser))
}
