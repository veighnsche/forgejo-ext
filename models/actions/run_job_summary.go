// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/util"
)

// The per-job limit is tied to the underlying api's body limit.
// Exceeding characters are truncated to conform to this.
const MaxJobSummarySize = 1024 * 1024

// ActionRunJobSummary holds the GITHUB_STEP_SUMMARY markdown produced by one attempt of a single job.
type ActionRunJobSummary struct {
	ID          int64 `xorm:"pk autoincr"`
	JobID       int64 `xorm:"unique(job_attempt)"`
	Attempt     int64 `xorm:"unique(job_attempt)"`
	RunID       int64
	RepoID      int64
	Content     string             `xorm:"LONGTEXT"`
	CreatedUnix timeutil.TimeStamp `xorm:"created"`
	UpdatedUnix timeutil.TimeStamp `xorm:"updated"`
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
	// todo: Figure out where/if to log why we're truncating now and maybe display the last few characters (after masking!)
	if len(summary.Content) > MaxJobSummarySize {
		summary.Content = summary.Content[:MaxJobSummarySize]
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		existing, err := GetJobSummary(ctx, summary.JobID, summary.Attempt)
		if err != nil && err != util.ErrNotExist {
			return err
		}
		if err == util.ErrNotExist {
			_, err := db.GetEngine(ctx).Insert(summary)
			return err
		}
		summary.UpdatedUnix = timeutil.TimeStampNow()
		_, err = db.GetEngine(ctx).ID(existing.ID).Cols("content", "updated_unix").Update(summary)
		return err
	})
}

func DeleteJobSummaries(ctx context.Context, jobID int64) error {
	_, err := db.GetEngine(ctx).Delete(&ActionRunJobSummary{JobID: jobID})
	return err
}
