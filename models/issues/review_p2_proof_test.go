// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues_test

// P2 proof for factory-workflow-1 FW03 (Lane C).
//
// This fixture exercises only native collaboration primitives; it encodes no
// Soda planning, severity, disposition or Jev policy. Full diff-anchor
// validation against the candidate diff and the conditional resolve/merge
// operations are FW16/FW17 work; this proof establishes the native behavior
// those operations build on:
//
//   - added-line (+) and deleted-line (-) code findings anchor on the
//     expected head via the native sign convention and read back stably;
//   - the summary review and its code comments commit atomically and a stale
//     head leaves no partial rows;
//   - thread resolution is permission-gated (CanMarkConversation) with
//     exact readback (ResolveDoer);
//   - reviewer officiality, a merge-blocker input, is actor-bound.

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestP2InlineFindingsAtomicBundleAndStableReadback submits one exact review
// bound to its head/base, then bundles an added-line and a deleted-line code
// finding in the same outer transaction. Both anchors read back with the
// correct native side, and repeated reads return the identical receipt.
func TestP2InlineFindingsAtomicBundleAndStableReadback(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 11})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.NoError(t, issue.LoadRepo(ctx))

	review, comm, err := issues_model.SubmitExactReview(ctx, doer, issue, issues_model.ReviewTypeReject, "two inline findings", exactTargetForIssue11())
	require.NoError(t, err)
	require.NotNil(t, review)
	require.NotNil(t, comm)
	require.Equal(t, exactHeadOID, review.CommitID)
	// Atomic receipt: summary review and its review comment share identity.
	require.Equal(t, review.ID, comm.ReviewID)

	addedPatch := "@@ -1,2 +1,3 @@\n context\n+added line under review\n context"
	deletedPatch := "@@ -1,3 +1,2 @@\n context\n-removed line under review\n context"
	added, err := issues_model.CreateComment(ctx, &issues_model.CreateCommentOptions{
		Type:      issues_model.CommentTypeCode,
		Doer:      doer,
		Repo:      issue.Repo,
		Issue:     issue,
		TreePath:  "README.md",
		LineNum:   4,
		CommitSHA: exactHeadOID,
		Patch:     addedPatch,
		ReviewID:  review.ID,
		Content:   "added-line finding: handle the new branch",
	})
	require.NoError(t, err)
	deleted, err := issues_model.CreateComment(ctx, &issues_model.CreateCommentOptions{
		Type:      issues_model.CommentTypeCode,
		Doer:      doer,
		Repo:      issue.Repo,
		Issue:     issue,
		TreePath:  "README.md",
		LineNum:   -2,
		CommitSHA: exactHeadOID,
		Patch:     deletedPatch,
		ReviewID:  review.ID,
		Content:   "deleted-line finding: removal drops a guard",
	})
	require.NoError(t, err)
	require.Equal(t, review.ID, added.ReviewID)
	require.Equal(t, review.ID, deleted.ReviewID)

	// Native anchor convention: positive lines are the proposed (new) side,
	// negative lines the previous (old/deleted) side.
	require.Equal(t, "proposed", added.DiffSide())
	require.Equal(t, "previous", deleted.DiffSide())
	require.Equal(t, uint64(4), added.UnsignedLine())
	require.Equal(t, uint64(2), deleted.UnsignedLine())

	// Stable readback: the anchored thread of each finding resolves to the
	// exact comment written, repeatedly (replay-safe observation).
	for range 3 {
		added.Issue = issue
		gotAdded, err := issues_model.FetchCodeConversation(ctx, added, doer)
		require.NoError(t, err)
		require.Len(t, gotAdded, 1)
		assert.Equal(t, added.ID, gotAdded[0].ID)
		assert.Equal(t, "added-line finding: handle the new branch", gotAdded[0].Content)

		deleted.Issue = issue
		gotDeleted, err := issues_model.FetchCodeConversation(ctx, deleted, doer)
		require.NoError(t, err)
		require.Len(t, gotDeleted, 1)
		assert.Equal(t, deleted.ID, gotDeleted[0].ID)
		assert.Equal(t, "deleted-line finding: removal drops a guard", gotDeleted[0].Content)
	}
}

// TestP2StaleHeadWritesNothing proves the exact-head gate refuses the whole
// submission: a changed head yields "head changed" and no review or comment
// row survives (the model joins the caller's transaction, so nothing commits).
func TestP2StaleHeadWritesNothing(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 11})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	before, err := issues_model.FindReviews(ctx, issues_model.FindReviewOptions{
		IssueID: 11, ReviewerID: 2,
		Types: []issues_model.ReviewType{issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject},
	})
	require.NoError(t, err)
	require.Empty(t, before)

	stale := exactTargetForIssue11()
	stale.CurrentHeadOID = "3333333333333333333333333333333333333333"
	_, _, err = issues_model.SubmitExactReview(ctx, doer, issue, issues_model.ReviewTypeApprove, "stale attempt", stale)
	require.Error(t, err)
	require.True(t, issues_model.IsErrExactReviewRefused(err))
	assert.Contains(t, err.Error(), "head changed")

	after, err := issues_model.FindReviews(ctx, issues_model.FindReviewOptions{
		IssueID: 11, ReviewerID: 2,
		Types: []issues_model.ReviewType{issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject},
	})
	require.NoError(t, err)
	assert.Empty(t, after)

	_, err = issues_model.GetCurrentReview(ctx, doer, issue)
	require.Error(t, err)
	assert.True(t, issues_model.IsErrReviewNotExist(err))
}

// TestP2ThreadResolutionPermissionsAndReadback resolves one exact code thread
// as an authorized actor, proves an outsider is denied by the permission
// check, and confirms exact resolver readback with stable re-resolution.
func TestP2ThreadResolutionPermissionsAndReadback(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 11})
	require.NoError(t, issue.LoadRepo(ctx))
	resolver := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	outsider := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})

	// user2 holds write access on repo1, so resolution is permitted; user5
	// has no grant there, so the same check denies.
	allowed, err := issues_model.CanMarkConversation(ctx, issue, resolver)
	require.NoError(t, err)
	require.True(t, allowed)
	denied, err := issues_model.CanMarkConversation(ctx, issue, outsider)
	require.NoError(t, err)
	require.False(t, denied)

	thread, err := issues_model.CreateComment(ctx, &issues_model.CreateCommentOptions{
		Type:      issues_model.CommentTypeCode,
		Doer:      resolver,
		Repo:      issue.Repo,
		Issue:     issue,
		TreePath:  "README.md",
		LineNum:   4,
		CommitSHA: exactHeadOID,
		Content:   "exact thread under test",
	})
	require.NoError(t, err)

	require.NoError(t, issues_model.MarkConversation(ctx, thread, resolver, true))
	resolved := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: thread.ID})
	require.Equal(t, resolver.ID, resolved.ResolveDoerID)
	require.NoError(t, issues_model.CommentList{resolved}.LoadResolveDoers(ctx))
	require.NotNil(t, resolved.ResolveDoer)
	assert.Equal(t, resolver.ID, resolved.ResolveDoer.ID)

	// Re-resolution is stable: no error, no ownership change.
	require.NoError(t, issues_model.MarkConversation(ctx, resolved, resolver, true))
	again := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: thread.ID})
	assert.Equal(t, resolver.ID, again.ResolveDoerID)
}

// TestP2ReviewerOfficialityIsActorBound shows a merge-blocker input (whether
// a review counts toward approvals) differs per actor on the same issue:
// user2's exact review is official while user4, without write access, stays
// unofficial. Native merge policy consumes this per-actor signal.
func TestP2ReviewerOfficialityIsActorBound(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	writer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	reader := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	official, err := issues_model.IsOfficialReviewer(ctx, issue, writer)
	require.NoError(t, err)
	assert.True(t, official)

	unofficial, err := issues_model.IsOfficialReviewer(ctx, issue, reader)
	require.NoError(t, err)
	assert.False(t, unofficial)
}
