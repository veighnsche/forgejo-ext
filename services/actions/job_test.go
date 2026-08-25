// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"testing"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/unittest"

	"code.forgejo.org/forgejo/runner/v13/act/jobparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteJobsOfRun(t *testing.T) {
	t.Run("Deletes completed job", func(t *testing.T) {
		defer unittest.OverrideFixtures("services/actions/TestDeleteJobsOfRun")()
		require.NoError(t, unittest.PrepareTestDatabase())

		run := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: 34901})
		job := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRunJob{ID: 47301, RunID: run.ID})
		unittest.AssertCount(t, &actions_model.ActionTask{JobID: job.ID}, 1)

		require.NoError(t, deleteJobsOfRun(t.Context(), run.ID))

		unittest.AssertNotExistsBean(t, &actions_model.ActionRunJob{ID: job.ID})
		unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRunJob{ID: 47302})
		unittest.AssertCount(t, &actions_model.ActionTask{JobID: job.ID}, 0)
	})

	t.Run("Error if job has not completed", func(t *testing.T) {
		defer unittest.OverrideFixtures("services/actions/TestDeleteJobsOfRun")()
		require.NoError(t, unittest.PrepareTestDatabase())

		run := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: 34902})
		job := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRunJob{ID: 47302, RunID: run.ID})
		unittest.AssertCount(t, &actions_model.ActionTask{JobID: job.ID}, 1)

		err := deleteJobsOfRun(t.Context(), run.ID)
		require.ErrorContains(t, err, "unable to delete job 47302 because it has not completed yet")

		unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRunJob{ID: job.ID})
		unittest.AssertCount(t, &actions_model.ActionTask{JobID: job.ID}, 1)
	})
}

func TestConvertSingleWorkflowToJobs(t *testing.T) {
	t.Run("Incomplete matrix", func(t *testing.T) {
		runDoesNotNeedApproval := &actions_model.ActionRun{
			RepoID:              int64(10),
			PullRequestID:       int64(2),
			PullRequestPosterID: int64(4),
		}

		workflowRaw := []byte(`
jobs:
  job2:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        dim1: "${{ fromJSON(needs.other-job.outputs.some-output) }}"
    steps:
      - run: true
`)
		workflows, err := jobparser.Parse(workflowRaw, false, jobparser.WithJobOutputs(map[string]map[string]string{}))
		require.NoError(t, err)
		require.True(t, workflows[0].IncompleteMatrix) // must be set for this test scenario to be valid

		jobs, err := convertSingleWorkflowToJobs(runDoesNotNeedApproval, workflows)
		require.NoError(t, err)
		require.Len(t, jobs, 1)

		// Expect job with an incomplete matrix to be StatusBlocked:
		assert.Equal(t, actions_model.StatusBlocked, jobs[0].Status)
	})

	t.Run("Incomplete runs-on", func(t *testing.T) {
		runDoesNotNeedApproval := &actions_model.ActionRun{
			RepoID:              int64(10),
			PullRequestID:       int64(2),
			PullRequestPosterID: int64(4),
		}

		workflowRaw := []byte(`
jobs:
  job2:
    runs-on: ${{ needs.other-job.outputs.some-output }}
    steps:
      - run: true
`)
		workflows, err := jobparser.Parse(workflowRaw, false, jobparser.WithJobOutputs(map[string]map[string]string{}), jobparser.SupportIncompleteRunsOn())
		require.NoError(t, err)
		require.True(t, workflows[0].IncompleteRunsOn) // must be set for this test scenario to be valid

		jobs, err := convertSingleWorkflowToJobs(runDoesNotNeedApproval, workflows)
		require.NoError(t, err)
		require.Len(t, jobs, 1)

		// Expect job with an incomplete runs-on to be StatusBlocked:
		assert.Equal(t, actions_model.StatusBlocked, jobs[0].Status)
	})

	t.Run("Incomplete with", func(t *testing.T) {
		runDoesNotNeedApproval := &actions_model.ActionRun{
			RepoID:              int64(10),
			PullRequestID:       int64(2),
			PullRequestPosterID: int64(4),
		}

		workflowRaw := []byte(`
jobs:
  outer-job:
    with:
      some_input: ${{ needs.other-job.outputs.some-output }}
    uses: ./.forgejo/workflows/reusable.yml
`)
		workflows, err := jobparser.Parse(workflowRaw, false,
			jobparser.WithJobOutputs(map[string]map[string]string{}),
			jobparser.ExpandLocalReusableWorkflows(func(job *jobparser.Job, path string) ([]byte, error) {
				return []byte(`
on:
  workflow_call:
    inputs:
      some_input:
        type: string
jobs:
  inner-job:
    runs-on: debian
    steps: []
`), nil
			}))
		require.NoError(t, err)
		require.True(t, workflows[0].IncompleteWith) // must be set for this test scenario to be valid

		jobs, err := convertSingleWorkflowToJobs(runDoesNotNeedApproval, workflows)
		require.NoError(t, err)
		require.Len(t, jobs, 1)

		// Expect job with an incomplete with to be StatusBlocked:
		assert.Equal(t, actions_model.StatusBlocked, jobs[0].Status)
	})
}

func TestConvertSingleWorkflowToJobs_FindOuterWorkflowCall(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	run := &actions_model.ActionRun{
		RepoID:              int64(10),
		PullRequestID:       int64(2),
		PullRequestPosterID: int64(4),
	}

	workflowRaw := []byte(`
jobs:
  outer-job:
    uses: ./.forgejo/workflows/reusable.yml
`)
	workflows, err := jobparser.Parse(workflowRaw, false,
		jobparser.WithJobOutputs(map[string]map[string]string{}),
		jobparser.ExpandLocalReusableWorkflows(func(job *jobparser.Job, path string) ([]byte, error) {
			return []byte(`
on:
  workflow_call:
jobs:
  inner-job-1:
    runs-on: debian
    steps: []
  inner-job-2:
    runs-on: debian
    steps: []
`), nil
		}))
	require.NoError(t, err)

	jobs, err := convertSingleWorkflowToJobs(run, workflows)
	require.NoError(t, err)
	require.Len(t, jobs, 3)

	require.NoError(t, actions_model.InsertRunWithoutNotification(t.Context(), run, jobs))

	for _, j := range jobs {
		t.Run(j.Name, func(t *testing.T) {
			_, err := j.DecodeWorkflowPayload()
			require.NoError(t, err)
			outer, err := run.FindOuterWorkflowCall(t.Context(), j)
			if j.Name == "outer-job" {
				require.ErrorContains(t, err, "invalid state for FindOuterWorkflowCall")
			} else {
				require.NoError(t, err)
				require.NotNil(t, outer)
				assert.Equal(t, "outer-job", outer.Name)
			}
		})
	}
}
