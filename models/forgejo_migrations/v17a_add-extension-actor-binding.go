// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add extension_actor_binding table",
		Upgrade:     addExtensionActorBinding,
	})
}

func addExtensionActorBinding(x *xorm.Engine) error {
	type ActorBinding struct {
		ID             int64              `xorm:"pk autoincr"`
		InstallationID string             `xorm:"VARCHAR(36) NOT NULL index unique(binding)"`
		TokenID        int64              `xorm:"NOT NULL index unique(binding)"`
		ActorID        int64              `xorm:"NOT NULL index unique(binding)"`
		RepositoryID   int64              `xorm:"NOT NULL index unique(binding)"`
		Kind           string             `xorm:"VARCHAR(64) NOT NULL index unique(binding)"`
		CreatedUnix    timeutil.TimeStamp `xorm:"created NOT NULL"`
		UpdatedUnix    timeutil.TimeStamp `xorm:"updated NOT NULL"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(ActorBinding))
	return err
}
