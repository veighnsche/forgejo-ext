// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add native operation ledger and mutation reservation tables",
		Upgrade:     addNativeOperation,
	})
}

func addNativeOperation(x *xorm.Engine) error {
	type Operation struct {
		ID                     int64              `xorm:"pk autoincr"`
		InstallationID         string             `xorm:"VARCHAR(36) NOT NULL index unique(op)"`
		OperationID            string             `xorm:"VARCHAR(128) NOT NULL index unique(op)"`
		Kind                   string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
		ActorID                int64              `xorm:"NOT NULL DEFAULT 0"`
		RepositoryID           int64              `xorm:"NOT NULL DEFAULT 0"`
		TokenID                int64              `xorm:"NOT NULL DEFAULT 0"`
		CredentialFingerprint  string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
		AuthRevision           string             `xorm:"TEXT NOT NULL"`
		ExpectedNativeRevision int64              `xorm:"NOT NULL DEFAULT 0"`
		NotAfter               int64              `xorm:"NOT NULL DEFAULT 0"`
		IntentDigest           string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
		Intent                 string             `xorm:"TEXT NOT NULL"`
		Submitted              bool               `xorm:"NOT NULL DEFAULT false"`
		Revoked                bool               `xorm:"NOT NULL DEFAULT false"`
		Admitted               bool               `xorm:"NOT NULL DEFAULT false"`
		EffectState            string             `xorm:"VARCHAR(16) NOT NULL DEFAULT 'pending'"`
		Reason                 string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
		Cancellation           string             `xorm:"VARCHAR(16) NOT NULL DEFAULT 'none'"`
		Completion             string             `xorm:"VARCHAR(16) NOT NULL DEFAULT ''"`
		Receipt                string             `xorm:"TEXT NOT NULL"`
		CreatedUnix            timeutil.TimeStamp `xorm:"created NOT NULL"`
		UpdatedUnix            timeutil.TimeStamp `xorm:"updated NOT NULL"`
	}
	type Reservation struct {
		ID          int64              `xorm:"pk"`
		Revision    int64              `xorm:"NOT NULL"`
		Owner       string             `xorm:"TEXT NOT NULL"`
		Generation  int64              `xorm:"NOT NULL DEFAULT 0"`
		OwnerKind   string             `xorm:"VARCHAR(16) NOT NULL DEFAULT ''"`
		Verifier    string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
		Scope       string             `xorm:"TEXT NOT NULL"`
		UpdatedUnix timeutil.TimeStamp `xorm:"updated NOT NULL"`
	}
	if _, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(Operation), new(Reservation)); err != nil {
		return err
	}
	// Seed the idle reservation idempotently through the mapped bean: raw
	// INSERT OR IGNORE is SQLite-only and omits the NOT NULL updated
	// timestamp that PostgreSQL enforces. Migrations run once per
	// deployment, so check-then-insert needs no further concurrency guard.
	has, err := x.ID(1).NoAutoCondition().Exist(new(Reservation))
	if err != nil {
		return err
	}
	if !has {
		if _, err := x.Insert(&Reservation{ID: 1, Revision: 1}); err != nil {
			return err
		}
	}
	return nil
}
