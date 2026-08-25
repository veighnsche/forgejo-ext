// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add the federated_repo_follower table",
		Upgrade:     addFederatedRepoFollower,
	})
}

func addFederatedRepoFollower(x *xorm.Engine) error {
	type FederatedRepoFollower struct {
		ID     int64 `xorm:"pk autoincr"`
		RepoID int64 `xorm:"NOT NULL unique(frf_rel)"`
		UserID int64 `xorm:"NOT NULL unique(frf_rel)"`
	}

	return x.Sync(&FederatedRepoFollower{})
}
