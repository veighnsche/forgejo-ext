// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import "code.forgejo.org/xorm/xorm"

func init() {
	registerMigration(&Migration{
		Description: "add digest column to attachment table",
		Upgrade:     addAttachmentDigest,
	})
}

func addAttachmentDigest(x *xorm.Engine) error {
	type Attachment struct {
		Digest string `xorm:"TEXT"`
	}

	// TODO: This does not recalculate digests!!!
	_, err := x.SyncWithOptions(
		xorm.SyncOptions{IgnoreDropIndices: true},
		new(Attachment),
	)
	return err
}
