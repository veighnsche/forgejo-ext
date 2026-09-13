// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
	"xorm.io/builder"
)

func init() {
	registerMigration(&Migration{
		Description: "Add foreign key to table lfs_meta_object",
		Upgrade:     addForeignKeyLFSMetaObject,
	})
}

func addForeignKeyLFSMetaObject(x *xorm.Engine) error {
	type Pointer struct {
		Oid string `json:"oid" xorm:"UNIQUE(s) INDEX NOT NULL"`
	}

	type LFSMetaObject struct {
		ID           int64 `xorm:"pk autoincr"`
		Pointer      `xorm:"extends"`
		RepositoryID int64 `xorm:"UNIQUE(s) INDEX NOT NULL REFERENCES(repository, id)"`
	}

	return syncForeignKeyWithDelete(x,
		new(LFSMetaObject),
		builder.Expr("NOT EXISTS (SELECT id FROM repository WHERE repository.id = lfs_meta_object.repository_id)"),
	)
}
