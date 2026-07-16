// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
	service_message "forgejo.org/modules/service_message"
)

func init() {
	registerMigration(&Migration{
		Description: "add confirmation info to user table",
		Upgrade:     addConfirmInfoToUser,
	})
}

func addConfirmInfoToUser(x *xorm.Engine) error {
	type User struct {
		ID       int64 `xorm:"pk autoincr"`
		Confirms service_message.ConfirmTimestamps
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, &User{})
	return err
}
