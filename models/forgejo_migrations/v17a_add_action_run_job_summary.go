// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
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
		ID      int64  `xorm:"pk autoincr"`
		JobID   int64  `xorm:"unique(job_attempt) NOT NULL REFERENCES(action_run_job, id)"`
		Attempt int64  `xorm:"unique(job_attempt) NOT NULL"`
		RunID   int64  `xorm:"NOT NULL REFERENCES(action_run, id)"`
		RepoID  int64  `xorm:"NOT NULL REFERENCES(repository, id)"`
		Content string `xorm:"LONGTEXT NOT NULL"`
	}

	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(ActionRunJobSummary))
	return err
}
