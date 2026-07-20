// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgejo_migrations

import (
	"forgejo.org/modules/setting"

	"code.forgejo.org/xorm/xorm"
)

func AddIsCodeIndexerEnabledToRepository(x *xorm.Engine) error {
	type Repository struct {
		ID                   int64 `xorm:"pk autoincr"`
		IsCodeIndexerEnabled bool  `xorm:"NOT NULL DEFAULT true"`
	}

	if err := x.SyncWithOptions(xorm.SyncOptions{
		IgnoreDropIndices: true,
	}, &Repository{}); err != nil {
		return err
	}

	_, err := x.Exec("UPDATE `repository` SET is_code_indexer_enabled = ?", setting.Indexer.RepoIndexerDefaultEnabled)
	return err
}

func init() {
	registerMigration(&Migration{
		Description: "Add is_code_indexer_enabled to repository",
		Upgrade:     AddIsCodeIndexerEnabledToRepository,
	})
}
