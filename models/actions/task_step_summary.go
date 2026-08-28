// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/log"
	"forgejo.org/modules/util"
)

// Step summaries beyond this size are truncated, matching the limit the runner enforces per step.
const MaxStepSummarySize = 1024 * 1024

// ActionTaskStepSummary holds the GITHUB_STEP_SUMMARY markdown produced by a single step of an ActionTask.
type ActionTaskStepSummary struct {
	ID      int64  `xorm:"pk autoincr"`
	StepID  int64  `xorm:"UNIQUE NOT NULL REFERENCES(action_task_step, id)"`
	TaskID  int64  `xorm:"INDEX NOT NULL"`
	RepoID  int64  `xorm:"INDEX NOT NULL"`
	Content string `xorm:"MEDIUMTEXT NOT NULL"`
}

func init() {
	db.RegisterModel(new(ActionTaskStepSummary))
}

// GetTaskStepSummaries returns the step summaries of the task, in no particular order.
func GetTaskStepSummaries(ctx context.Context, taskID int64) ([]*ActionTaskStepSummary, error) {
	var summaries []*ActionTaskStepSummary
	return summaries, db.GetEngine(ctx).Where("task_id = ?", taskID).Find(&summaries)
}

// SetTaskStepSummaries upserts the collected stepSummaries to the task but truncates them if they exceed 1MiB of size.
func SetTaskStepSummaries(ctx context.Context, summaries []*ActionTaskStepSummary) error {
	return db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		for _, summary := range summaries {
			if len(summary.Content) > MaxStepSummarySize {
				log.Warn("Summary of step %d of task %d exceeds the size limit of %d bytes and is truncated, (the runner should have truncated it already tho)",
					summary.StepID, summary.TaskID, MaxStepSummarySize)
				summary.Content, _ = util.SplitStringAtByteN(summary.Content, MaxStepSummarySize)
			}
			existing := &ActionTaskStepSummary{}
			has, err := e.Where("step_id = ?", summary.StepID).Get(existing)
			if err != nil {
				return err
			}
			if has {
				summary.ID = existing.ID
				if _, err := e.ID(existing.ID).Cols("content").Update(summary); err != nil {
					return err
				}
			} else if _, err := e.Insert(summary); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteTaskStepSummaries removes the step summaries of the task. Due to the foreign key on StepID it has to run
// before the steps of the task are deleted.
func DeleteTaskStepSummaries(ctx context.Context, taskID int64) error {
	_, err := db.GetEngine(ctx).Delete(&ActionTaskStepSummary{TaskID: taskID})
	return err
}
