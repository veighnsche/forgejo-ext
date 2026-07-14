// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add action_run_job_summary table",
		Upgrade:     addActionRunJobSummary,
	})
}

func addActionRunJobSummary(x *xorm.Engine) error {
	type ActionRunJobSummary struct {
		ID          int64              `xorm:"pk autoincr"`
		JobID       int64              `xorm:"unique(job_attempt)"`
		Attempt     int64              `xorm:"unique(job_attempt)"`
		RunID       int64              `xorm:"index"`
		RepoID      int64              `xorm:"index"`
		Content     string             `xorm:"LONGTEXT"`
		CreatedUnix timeutil.TimeStamp `xorm:"created"`
		UpdatedUnix timeutil.TimeStamp `xorm:"updated"`
	}

	return x.Sync(new(ActionRunJobSummary))
}
