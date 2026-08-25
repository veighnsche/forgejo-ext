// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add the federated_mirror table",
		Upgrade:     addFederatedMirror,
	})
}

func addFederatedMirror(x *xorm.Engine) error {
	type FederatedMirror struct {
		ID             int64  `xorm:"pk autoincr"`
		RepoID         int64  `xorm:"repo_id NOT NULL UNIQUE"`
		RemoteActorURI string `xorm:"remote_actor_uri NOT NULL"`
		RemoteCloneURI string `xorm:"remote_clone_uri NOT NULL"`
		IsPush         bool   `xorm:"is_push NOT NULL DEFAULT false"`
	}

	return x.Sync(&FederatedMirror{})
}
