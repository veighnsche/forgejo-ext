// Copyright 2024 The Forgejo Authors
// SPDX-License-Identifier: MIT

package integration

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	nativeoperation_model "forgejo.org/models/nativeoperation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/queue"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/test"
	"forgejo.org/modules/timeutil"
	pull_service "forgejo.org/services/pull"
	repo_service "forgejo.org/services/repository"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullRequestSynchronized(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// unmerged pull request of user2/repo1 from branch2 to master
	pull := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	// tip of tests/gitea-repositories-meta/user2/repo1 branch2
	pull.HeadCommitID = "985f0301dba5e7b34be866819cd15ad3d8f508ee"
	pull.LoadIssue(db.DefaultContext)
	pull.Issue.Created = timeutil.TimeStampNanoNow()
	issues_model.UpdateIssueCols(db.DefaultContext, pull.Issue, "created")

	require.Equal(t, pull.HeadRepoID, pull.BaseRepoID)
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: pull.HeadRepoID})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID})

	for _, testCase := range []struct {
		name     string
		timeNano int64
		expected bool
	}{
		{
			name:     "AddTestPullRequestTask process PR",
			timeNano: int64(pull.Issue.Created),
			expected: true,
		},
		{
			name:     "AddTestPullRequestTask skip PR",
			timeNano: 0,
			expected: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.NoError(t, queue.GetManager().FlushAll(t.Context(), time.Minute))
			logChecker, cleanup := test.NewLogChecker(log.DEFAULT, log.TRACE)
			logChecker.Filter("Updating PR").StopMark("TestPullRequest ")
			defer cleanup()

			opt := &repo_module.PushUpdateOptions{
				PusherID:     owner.ID,
				PusherName:   owner.Name,
				RepoUserName: owner.Name,
				RepoName:     repo.Name,
				RefFullName:  git.RefName("refs/heads/branch2"),
				OldCommitID:  pull.HeadCommitID,
				NewCommitID:  pull.HeadCommitID,
				TimeNano:     testCase.timeNano,
			}
			require.NoError(t, repo_service.PushUpdate(opt))
			logFiltered, logStopped := logChecker.Check(10 * time.Second)
			assert.True(t, logStopped)
			assert.Equal(t, testCase.expected, logFiltered[0])
		})
	}

	for _, testCase := range []struct {
		name      string
		olderThan int64
		expected  bool
	}{
		{
			name:      "TestPullRequest process PR",
			olderThan: int64(pull.Issue.Created),
			expected:  true,
		},
		{
			name:      "TestPullRequest skip PR",
			olderThan: int64(pull.Issue.Created) - 1,
			expected:  false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.NoError(t, queue.GetManager().FlushAll(t.Context(), time.Minute))
			logChecker, cleanup := test.NewLogChecker(log.DEFAULT, log.TRACE)
			logChecker.Filter("Updating PR").StopMark("TestPullRequest ")
			defer cleanup()

			pull_service.TestPullRequest(t.Context(), owner, repo.ID, testCase.olderThan, "branch2", true, pull.HeadCommitID, pull.HeadCommitID)
			logFiltered, logStopped := logChecker.Check(10 * time.Second)
			assert.True(t, logStopped)
			assert.Equal(t, testCase.expected, logFiltered[0])
		})
	}
}

// A push still owns the reservation when it launches its deferred PR sweep.
// The sweep must wait independently, then synchronize without another push.
func TestPullRequestTaskRetainsBusySweep(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	pull := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: pull.HeadRepoID})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID})
	checker, cleanup := test.NewLogChecker(log.DEFAULT, log.TRACE)
	defer cleanup()
	checker.Filter("Updating PR").StopMark("TestPullRequest ")
	const held = "ord:collaboration/batch/pr-sweep/0123456789abcdef"
	_, err := nativeoperation_model.ClaimOrdinary(t.Context(), held, "{}", "held-sweep-verifier")
	require.NoError(t, err)
	defer func() { _ = nativeoperation_model.ReleaseOwner(t.Context(), held) }()
	pull_service.AddTestPullRequestTask(t.Context(), owner, repo.ID, "branch2", true, pull.HeadCommitID, pull.HeadCommitID, int64(timeutil.TimeStampNanoNow()))
	filtered, stopped := checker.Check(250 * time.Millisecond)
	require.False(t, stopped, "a held reservation must fence the sweep")
	require.False(t, filtered[0])
	require.NoError(t, nativeoperation_model.ReleaseOwner(t.Context(), held))
	filtered, stopped = checker.Check(10 * time.Second)
	require.True(t, stopped, "the same deferred task must resume after release")
	require.True(t, filtered[0], "the resumed task must process its PR")
}
