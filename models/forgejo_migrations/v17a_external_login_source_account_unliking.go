// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "external login source account unlinking",
		Upgrade:     externalLoginSourceAccountUnlinking,
	})
}

func externalLoginSourceAccountUnlinking(x *xorm.Engine) error {
	type Source struct {
		AllowUnlinking bool `xorm:"NOT NULL DEFAULT false"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(Source))
	return err
}
