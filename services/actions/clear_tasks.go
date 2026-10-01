// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package actions

import (
	"context"
	"fmt"
	"time"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	"forgejo.org/modules/actions"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	operation_service "forgejo.org/services/nativeoperation"
)

// StopZombieTasks stops the task which have running status, but haven't been updated for a long time
func StopZombieTasks(ctx context.Context) error {
	return stopTasks(ctx, actions_model.FindTaskOptions{
		Status:        []actions_model.Status{actions_model.StatusRunning},
		UpdatedBefore: timeutil.TimeStamp(time.Now().Add(-setting.Actions.ZombieTaskTimeout).Unix()),
	})
}

// StopEndlessTasks stops the tasks which have running status and continuous updates, but don't end for a long time
func StopEndlessTasks(ctx context.Context) error {
	return stopTasks(ctx, actions_model.FindTaskOptions{
		Status:        []actions_model.Status{actions_model.StatusRunning},
		StartedBefore: timeutil.TimeStamp(time.Now().Add(-setting.Actions.EndlessTaskTimeout).Unix()),
	})
}

func stopTasks(ctx context.Context, opts actions_model.FindTaskOptions) error {
	tasks, err := db.Find[actions_model.ActionTask](ctx, opts)
	if err != nil {
		return fmt.Errorf("find tasks: %w", err)
	}

	for _, task := range tasks {
		// One sweep item owns its task/job change and resulting commit
		// status together: StopTask reuses this execution and the
		// status insert commits under the same owner, so a busy gate
		// skips the task entirely instead of stopping it without its
		// status. The log transfer stays outside: it is telemetry file
		// movement with its own retry, never under the reservation.
		if err := operation_service.Default().WithOrdinaryOwnership(ctx, operation_service.FamilyActionsTask, fmt.Sprintf("task/%d", task.ID), operation_service.Scope{
			TaskID: task.ID,
		}, func(ctx context.Context) error {
			return db.WithTx(ctx, func(ctx context.Context) error {
				if err := StopTask(ctx, task.ID, actions_model.StatusFailure); err != nil {
					return err
				}
				if err := task.LoadJob(ctx); err != nil {
					return err
				}
				CreateCommitStatus(ctx, task.Job)
				return nil
			})
		}); err != nil {
			log.Warn("Cannot stop task %v: %v", task.ID, err)
			continue
		}

		remove, err := actions.TransferLogs(ctx, task.LogFilename)
		if err != nil {
			log.Warn("Cannot transfer logs of task %v: %v", task.ID, err)
			continue
		}
		task.LogInStorage = true
		if err := actions_model.UpdateTask(ctx, task, "log_in_storage"); err != nil {
			log.Warn("Cannot update task %v: %v", task.ID, err)
			continue
		}
		remove()
	}

	return nil
}

// CancelAbandonedJobs cancels the jobs which have waiting status, but haven't been picked by a runner for a long time
func CancelAbandonedJobs(ctx context.Context) error {
	jobs, err := db.Find[actions_model.ActionRunJob](ctx, actions_model.FindRunJobOptions{
		Statuses:         []actions_model.Status{actions_model.StatusWaiting, actions_model.StatusBlocked},
		UpdatedBefore:    timeutil.TimeStamp(time.Now().Add(-setting.Actions.AbandonedJobTimeout).Unix()),
		RunNeedsApproval: optional.Some(false),
	})
	if err != nil {
		log.Warn("find abandoned tasks: %v", err)
		return err
	}

	now := timeutil.TimeStampNow()
	for _, job := range jobs {
		job.Status = actions_model.StatusCancelled
		job.Stopped = now
		// One sweep item owns its job change and resulting commit
		// status together; a busy gate skips only that job and the
		// next sweep retries it.
		if err := operation_service.Default().WithOrdinaryOwnership(ctx, operation_service.FamilyActionsRun, operation_service.RunResource(operation_service.ActionsRunOpSweep), operation_service.Scope{
			RunID: job.RunID,
			JobID: job.ID,
		}, func(ctx context.Context) error {
			if err := db.WithTx(ctx, func(ctx context.Context) error {
				_, err := UpdateRunJob(ctx, job, nil, "status", "stopped")
				return err
			}); err != nil {
				return err
			}
			CreateCommitStatus(ctx, job)
			return nil
		}); err != nil {
			log.Warn("cancel abandoned job %v: %v", job.ID, err)
			// go on
		}
	}

	return nil
}
