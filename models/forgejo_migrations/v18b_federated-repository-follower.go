// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add the federated_repository_follower table",
		Upgrade:     addFederatedRepositoryFollower,
	})
}

func addFederatedRepositoryFollower(x *xorm.Engine) error {
	type FederatedRepositoryFollower struct {
		ID             int64  `xorm:"pk autoincr"`
		RepoID         int64  `xorm:"repo_id NOT NULL unique(frf_rel)"`
		RemoteActorURI string `xorm:"remote_actor_uri NOT NULL unique(frf_rel)"`
		InboxURL       string `xorm:"inbox_url NOT NULL"`
	}

	return x.Sync(&FederatedRepositoryFollower{})
}
