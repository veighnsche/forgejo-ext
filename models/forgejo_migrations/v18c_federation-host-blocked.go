// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add blocked field to federation_host",
		Upgrade:     addFederationHostBlocked,
	})
}

func addFederationHostBlocked(x *xorm.Engine) error {
	type FederationHost struct {
		Blocked bool `xorm:"NOT NULL DEFAULT FALSE"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(FederationHost))
	return err
}
