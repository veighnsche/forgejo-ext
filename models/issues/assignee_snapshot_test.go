// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues_test

import (
	"testing"

	issues_model "forgejo.org/models/issues"

	"github.com/stretchr/testify/require"
)

func TestAssigneePageIsDeterministicAndComplete(t *testing.T) {
	page, err := issues_model.NewAssigneePage(9, []int64{5, 2, 8}, 3, true, "", true)
	require.NoError(t, err)
	require.True(t, page.Visible)
	require.True(t, page.Complete)
	require.Equal(t, int64(9), page.IssueID)
	require.Equal(t, []int64{2, 5, 8}, page.AssigneeIDs)
	require.Equal(t, 3, page.Total)

	empty, err := issues_model.NewAssigneePage(9, nil, 0, true, "", true)
	require.NoError(t, err)
	require.True(t, empty.Complete)
	require.Empty(t, empty.AssigneeIDs)

	hidden, err := issues_model.NewAssigneePage(9, []int64{2}, 1, false, issues_model.HiddenReasonNoAccess, true)
	require.NoError(t, err)
	require.False(t, hidden.Visible)
	require.Equal(t, int64(9), hidden.IssueID)
	require.Empty(t, hidden.AssigneeIDs)

	_, err = issues_model.NewAssigneePage(9, []int64{2}, 2, true, "", true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid, "missing assignee must not report complete")

	_, err = issues_model.NewAssigneePage(9, []int64{0}, 1, true, "", true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid, "zero assignee id must refuse")

	_, err = issues_model.NewAssigneePage(0, nil, 0, true, "", true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid, "missing issue locator must refuse")
}
