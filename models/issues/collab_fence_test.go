// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues

import (
	"testing"

	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/require"
)

func TestCollabWritersFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	claimed, err := nativeoperation.ClaimOrdinary(ctx, "ord:branch-delete/1/x", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	defer func() { _ = nativeoperation.ReleaseOwner(ctx, claimed.Owner) }()

	issue := unittest.AssertExistsAndLoadBean(t, &Issue{ID: 1})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	other := unittest.AssertExistsAndLoadBean(t, &Issue{ID: 2})

	require.ErrorIs(t, UpdateComment(ctx, &Comment{ID: 1}, 1, doer), nativeoperation.ErrBusy)
	require.ErrorIs(t, DeleteComment(ctx, &Comment{ID: 1}), nativeoperation.ErrBusy)
	require.ErrorIs(t, func() error {
		_, err := CreateReaction(ctx, &ReactionOptions{DoerID: doer.ID, IssueID: issue.ID})
		return err
	}(), nativeoperation.ErrBusy)
	require.ErrorIs(t, CreateIssueDependency(ctx, doer, issue, other), nativeoperation.ErrBusy)
	require.ErrorIs(t, func() error {
		_, _, err := ToggleIssueAssignee(ctx, issue, doer, doer.ID)
		return err
	}(), nativeoperation.ErrBusy)
	require.ErrorIs(t, LockIssue(ctx, &IssueLockOptions{Doer: doer, Issue: issue}), nativeoperation.ErrBusy)
	require.ErrorIs(t, issue.Pin(ctx, doer), nativeoperation.ErrBusy)
	require.ErrorIs(t, IssueAssignOrRemoveProject(ctx, issue, doer, 1, 0), nativeoperation.ErrBusy)
	require.ErrorIs(t, MarkConversation(ctx, &Comment{ID: 1, Type: CommentTypeCode}, doer, true), nativeoperation.ErrBusy)
	require.ErrorIs(t, func() error {
		_, _, err := SubmitReview(ctx, doer, issue, ReviewTypeComment, "x", "c0ffee", false, nil)
		return err
	}(), nativeoperation.ErrBusy)
	require.ErrorIs(t, UpdateIssueCols(ctx, issue, "name"), nativeoperation.ErrBusy)
	require.ErrorIs(t, SaveIssueContentHistory(ctx, doer.ID, issue.ID, 0, 1, "x", false), nativeoperation.ErrBusy)
}

func TestCollabWritersProceedWhenIdle(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	issue := unittest.AssertExistsAndLoadBean(t, &Issue{ID: 1})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.NoError(t, issue.LoadRepo(ctx))
	require.NoError(t, LockIssue(ctx, &IssueLockOptions{Doer: doer, Issue: issue}))
	locked := unittest.AssertExistsAndLoadBean(t, &Issue{ID: 1})
	require.True(t, locked.IsLocked)
}
