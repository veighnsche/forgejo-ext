// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/util"
)

// The per-job limit is tied to the underlying api's body limit.
// Exceeding characters are truncated to conform to this.
const MaxJobSummarySize = 1024 * 1024

// ActionRunJobSummary holds the GITHUB_STEP_SUMMARY markdown produced by one attempt of a single job.
type ActionRunJobSummary struct {
	ID      int64  `xorm:"pk autoincr"`
	JobID   int64  `xorm:"unique(job_attempt) NOT NULL REFERENCES(action_run_job, id)"`
	Attempt int64  `xorm:"unique(job_attempt) NOT NULL"`
	RunID   int64  `xorm:"NOT NULL REFERENCES(action_run, id)"`
	RepoID  int64  `xorm:"NOT NULL REFERENCES(repository, id)"`
	Content string `xorm:"LONGTEXT NOT NULL"`
}

func init() {
	db.RegisterModel(new(ActionRunJobSummary))
}

func GetJobSummary(ctx context.Context, jobID, attempt int64) (*ActionRunJobSummary, error) {
	summary := &ActionRunJobSummary{}
	has, err := db.GetEngine(ctx).Where("job_id = ? AND attempt = ?", jobID, attempt).Get(summary)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.ErrNotExist
	}
	return summary, nil
}

func SetJobSummary(ctx context.Context, summary *ActionRunJobSummary) error {
	summary.Content, _ = util.SplitStringAtByteN(summary.Content, MaxJobSummarySize)
	existing, err := GetJobSummary(ctx, summary.JobID, summary.Attempt)
	if err != nil && err != util.ErrNotExist {
		return err
	}
	if err == util.ErrNotExist {
		_, err := db.GetEngine(ctx).Insert(summary)
		return err
	}
	_, err = db.GetEngine(ctx).ID(existing.ID).Cols("content").Update(summary)
	return err
}

func DeleteJobSummaries(ctx context.Context, jobID int64) error {
	_, err := db.GetEngine(ctx).Delete(&ActionRunJobSummary{JobID: jobID})
	return err
}
