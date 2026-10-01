// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull_test

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDismissReview(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	pull := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{})
	require.NoError(t, pull.LoadIssue(db.DefaultContext))
	issue := pull.Issue
	require.NoError(t, issue.LoadRepo(db.DefaultContext))
	reviewer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	review, err := issues_model.CreateReview(db.DefaultContext, issues_model.CreateReviewOptions{
		Issue:    issue,
		Reviewer: reviewer,
		Type:     issues_model.ReviewTypeReject,
	})

	require.NoError(t, err)
	issue.IsClosed = true
	pull.HasMerged = false
	require.NoError(t, issues_model.UpdateIssueCols(db.DefaultContext, issue, "is_closed"))
	require.NoError(t, pull.UpdateCols(db.DefaultContext, "has_merged"))
	_, err = pull_service.DismissReview(db.DefaultContext, review.ID, issue.RepoID, "", &user_model.User{}, false, false)
	require.Error(t, err)
	assert.True(t, pull_service.IsErrDismissRequestOnClosedPR(err))

	pull.HasMerged = true
	pull.Issue.IsClosed = false
	require.NoError(t, issues_model.UpdateIssueCols(db.DefaultContext, issue, "is_closed"))
	require.NoError(t, pull.UpdateCols(db.DefaultContext, "has_merged"))
	_, err = pull_service.DismissReview(db.DefaultContext, review.ID, issue.RepoID, "", &user_model.User{}, false, false)
	require.Error(t, err)
	assert.True(t, pull_service.IsErrDismissRequestOnClosedPR(err))
}

func TestCompleteReviewSubmission(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	// Ordinary primary first, then the shared bounded completion.
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 11})
	require.NoError(t, issue.LoadRepo(ctx))
	require.NoError(t, issue.Repo.LoadOwner(ctx))
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	review, comm, err := issues_model.SubmitReview(ctx, doer, issue, issues_model.ReviewTypeApprove, "good @user4", "1111111111111111111111111111111111111111", false, nil)
	require.NoError(t, err)
	require.NoError(t, issue.LoadPullRequest(ctx))
	require.NoError(t, review.LoadCodeComments(ctx))

	require.NoError(t, pull_service.CompleteReviewSubmission(ctx, doer, issue, review, comm))
	// Completion only records mentions and notifications; repeating it is safe.
	require.NoError(t, pull_service.CompleteReviewSubmission(ctx, doer, issue, review, comm))

	mentioned, err := issues_model.ResolveIssueMentionsByVisibility(ctx, issue, doer, []string{"user4"})
	require.NoError(t, err)
	require.Len(t, mentioned, 1)
	assert.Equal(t, int64(4), mentioned[0].ID)
}
