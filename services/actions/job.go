// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"
	"errors"
	"fmt"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	"forgejo.org/modules/log"
	"forgejo.org/modules/util"

	"code.forgejo.org/forgejo/runner/v13/act/jobparser"
	gouuid "github.com/google/uuid"
)

// deleteJobsOfRun removes all jobs that belong to the given run, including its associated tasks. Each job has to be
// completed for the operation to succeed.
func deleteJobsOfRun(ctx context.Context, runID int64) error {
	return db.WithTx(ctx, func(ctx context.Context) error {
		jobs, err := actions_model.GetRunJobsByRunID(ctx, runID)
		if err != nil {
			return fmt.Errorf("unable to load jobs of run %d: %w", runID, err)
		}

		for _, job := range jobs {
			if !job.Status.IsDone() {
				return fmt.Errorf("unable to delete job %d because it has not completed yet", job.ID)
			}

			tasks, err := actions_model.GetTasksOfJob(ctx, job.ID)
			if err != nil {
				return err
			}
			for _, task := range tasks {
				err = deleteTask(ctx, task.ID)
				if err != nil {
					return err
				}
			}

			err = actions_model.DeleteJob(ctx, job.ID)
			if err != nil {
				return fmt.Errorf("unable to delete job %d of run %d: %w", job.ID, job.RunID, err)
			}
		}

		return nil
	})
}

func convertSingleWorkflowToJobs(run *actions_model.ActionRun, jobs []*jobparser.SingleWorkflow) ([]*actions_model.ActionRunJob, error) {
	runJobs := make([]*actions_model.ActionRunJob, 0, len(jobs))
	for _, v := range jobs {
		id, job := v.Job()
		status := actions_model.StatusFailure
		payload := []byte{}
		needs := []string{}
		name := run.Title
		runsOn := []string{}

		if job != nil {
			needs = job.Needs()
			if err := v.SetJob(id, job.EraseNeeds()); err != nil {
				return nil, err
			}
			payload, _ = v.Marshal()

			if len(needs) > 0 || run.NeedApproval || v.IncompleteMatrix || v.IncompleteRunsOn || v.IncompleteWith {
				status = actions_model.StatusBlocked
			} else if ifPassed, err := job.EvaluateIf(); err == nil && !ifPassed {
				log.Trace("job %q skipped by server-side 'if' evaluation", id)
				status = actions_model.StatusSkipped
			} else {
				if err != nil && !errors.Is(err, jobparser.ErrCannotEvaluateInJobParser) {
					return nil, fmt.Errorf("unable to evaluate job 'if' on server-side with unexpected error: %w", err)
				}
				status = actions_model.StatusWaiting
			}

			name, _ = util.SplitStringAtByteN(job.Name, 255)
			runsOn = job.RunsOn()
		}

		runJob := &actions_model.ActionRunJob{
			RunID:             run.ID,
			Run:               run,
			RepoID:            run.RepoID,
			OwnerID:           run.OwnerID,
			CommitSHA:         run.CommitSHA,
			IsForkPullRequest: run.IsForkPullRequest,
			Name:              name,
			WorkflowPayload:   payload,
			JobID:             id,
			Needs:             needs,
			RunsOn:            runsOn,
			Status:            status,
			Attempt:           1,
			Handle:            gouuid.New().String(),
		}

		runJobs = append(runJobs, runJob)
	}

	return runJobs, nil
}
