// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issue

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/organization"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteNotPassedAssignee(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	// Fake issue with assignees
	issue, err := issues_model.GetIssueByID(db.DefaultContext, 1)
	require.NoError(t, err)

	err = issue.LoadAttributes(db.DefaultContext)
	require.NoError(t, err)

	assert.Len(t, issue.Assignees, 1)

	user1, err := user_model.GetUserByID(db.DefaultContext, 1) // This user is already assigned (see the definition in fixtures), so running  UpdateAssignee should unassign him
	require.NoError(t, err)

	// Check if he got removed
	isAssigned, err := issues_model.IsUserAssignedToIssue(db.DefaultContext, issue, user1)
	require.NoError(t, err)
	assert.True(t, isAssigned)

	// Clean everyone
	err = DeleteNotPassedAssignee(db.DefaultContext, issue, user1, []*user_model.User{})
	require.NoError(t, err)
	assert.Empty(t, issue.Assignees)

	// Reload to check they're gone
	issue.ResetAttributesLoaded()
	require.NoError(t, issue.LoadAssignees(db.DefaultContext))
	assert.Empty(t, issue.Assignees)
	assert.Empty(t, issue.Assignee)
}

// A doer whose only access to an org-owned repo comes from team membership
// (not the PR's poster, not the repo-owner account, not an explicit
// collaborator row) must not crash IsValidTeamReviewRequest just because
// issue.Repo.Owner hasn't been preloaded.
func TestIsValidTeamReviewRequest_TeamOnlyAccessDoesNotPanic(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	// issue 23 / repo 25: a PR on an org-owned repo (org 17), authored by
	// user 2, who is unrelated to team 9 ("review_team", which has access
	// to repo 24 - see team_repo.yml) and has no collaboration row there.
	issue, err := issues_model.GetIssueByID(db.DefaultContext, 25)
	require.NoError(t, err)

	// Mirrors apiReviewRequest exactly: Issue.LoadRepo populates issue.Repo
	// but deliberately does NOT preload issue.Repo.Owner.
	require.NoError(t, issue.LoadRepo(db.DefaultContext))
	assert.Nil(t, issue.Repo.Owner, "fixture setup should leave Owner unloaded, matching production")

	team, err := organization.GetTeamByID(db.DefaultContext, 9)
	require.NoError(t, err)

	doer, err := user_model.GetUserByID(db.DefaultContext, 20) // team 9 member, not the poster
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		_ = IsValidTeamReviewRequest(db.DefaultContext, team, doer, true, issue)
	})
}
