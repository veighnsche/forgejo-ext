// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"testing"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullMergeRequiredStatusUsesSelectedCommit(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	pr.BaseBranch = "pinned-status-checks"
	require.NoError(t, pr.LoadBaseRepo(t.Context()))
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	unittest.AssertSuccessfulInsert(t, &git_model.ProtectedBranch{
		RepoID: pr.BaseRepoID, RuleName: pr.BaseBranch,
		EnableStatusCheck: true, StatusCheckContexts: []string{"required-ci"},
	})

	const selectedSHA = "1111111111111111111111111111111111111111"
	const otherSHA = "2222222222222222222222222222222222222222"
	pr.HeadCommitID = otherSHA
	for _, status := range []struct {
		sha   string
		state structs.CommitStatusState
	}{
		{selectedSHA, structs.CommitStatusFailure},
		{otherSHA, structs.CommitStatusSuccess},
	} {
		require.NoError(t, git_model.NewCommitStatus(t.Context(), git_model.NewCommitStatusOptions{
			Repo: pr.BaseRepo, Creator: doer, SHA: git.MustIDFromString(status.sha),
			CommitStatus: &git_model.CommitStatus{Context: "required-ci", State: status.state},
		}))
	}

	// A different head's successful checks must never approve the selected commit.
	passed, err := isPullCommitStatusPassForSHA(t.Context(), pr, selectedSHA)
	require.NoError(t, err)
	assert.False(t, passed)
	passed, err = isPullCommitStatusPassForSHA(t.Context(), pr, otherSHA)
	require.NoError(t, err)
	assert.True(t, passed)
	passed, err = isPullCommitStatusPassForSHA(t.Context(), pr, "3333333333333333333333333333333333333333")
	require.NoError(t, err)
	assert.False(t, passed, "missing required checks must block the merge")
}

func TestPullMergeWithChecksDeletedHeadRepository(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	pr.HeadRepoID = 999999
	_, err := db.GetEngine(t.Context()).ID(pr.ID).Cols("head_repo_id").Update(pr)
	require.NoError(t, err)
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	err = MergeWithChecks(t.Context(), pr, doer, nil, repo_model.MergeStyleMerge,
		"1111111111111111111111111111111111111111", pr.BaseBranch, nil)
	require.ErrorIs(t, err, util.ErrNotExist)
}
