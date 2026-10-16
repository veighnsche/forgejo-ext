// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues_test

// FW16 proof for factory-workflow-1 (Lane C): the generic conditional
// inline review primitive. The summary review and its keyed code comments
// commit atomically with stable replay; structural rules (usable keys and
// paths, old|new sides, one-based lines, unique keys, no duplicate
// anchors, bounded count) refuse before any write; stale candidates and
// outer rollbacks leave no partial rows. Diff membership (added, deleted,
// context and rename-side lines with binary refusal) is proven by the
// conditional caller against the live candidate diff; the model
// re-checks structure. Thread resolution stays a separate operation.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inlineAddedComment() issues_model.ExactReviewComment {
	return issues_model.ExactReviewComment{
		Key:   "f1",
		Path:  "README.md",
		Side:  issues_model.ExactReviewCommentSideNew,
		Line:  4,
		Body:  "added-line finding: handle the new branch",
		Patch: "@@ -1,2 +1,3 @@\n context\n+added line under review\n context",
	}
}

func inlineDeletedComment() issues_model.ExactReviewComment {
	return issues_model.ExactReviewComment{
		Key:   "f2",
		Path:  "README.md",
		Side:  issues_model.ExactReviewCommentSideOld,
		Line:  2,
		Body:  "deleted-line finding: removal drops a guard",
		Patch: "@@ -1,3 +1,2 @@\n context\n-removed line under review\n context",
	}
}

func countCodeComments(t *testing.T) int64 {
	t.Helper()
	n, err := db.GetEngine(db.DefaultContext).
		Where("issue_id = ? AND type = ?", 11, issues_model.CommentTypeCode).
		Count(new(issues_model.Comment))
	require.NoError(t, err)
	return n
}

func TestValidateExactReviewComments(t *testing.T) {
	valid := []issues_model.ExactReviewComment{inlineAddedComment(), inlineDeletedComment()}
	require.NoError(t, issues_model.ValidateExactReviewComments(valid))
	require.NoError(t, issues_model.ValidateExactReviewComments(nil))

	mutate := func(f func(*[]issues_model.ExactReviewComment)) []issues_model.ExactReviewComment {
		comments := []issues_model.ExactReviewComment{inlineAddedComment(), inlineDeletedComment()}
		f(&comments)
		return comments
	}
	many := make([]issues_model.ExactReviewComment, 0, issues_model.MaxExactReviewComments+1)
	for i := 0; i <= issues_model.MaxExactReviewComments; i++ {
		many = append(many, issues_model.ExactReviewComment{
			Key:  "k" + strings.Repeat("0", 10) + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Path: "a.go", Side: issues_model.ExactReviewCommentSideNew, Line: int64(i + 1), Body: "b",
		})
	}
	dupAnchor := inlineAddedComment()
	dupAnchor.Key = "f9"
	cases := []struct {
		name     string
		comments []issues_model.ExactReviewComment
		reason   string
	}{
		{"empty key", mutate(func(c *[]issues_model.ExactReviewComment) { (*c)[0].Key = "" }), "invalid inline comment"},
		{"key with space", mutate(func(c *[]issues_model.ExactReviewComment) { (*c)[0].Key = "has space" }), "invalid inline comment"},
		{"empty path", mutate(func(c *[]issues_model.ExactReviewComment) { (*c)[0].Path = "" }), "invalid inline comment"},
		{"absolute path", mutate(func(c *[]issues_model.ExactReviewComment) { (*c)[0].Path = "/a.go" }), "invalid inline comment"},
		{"dotdot path", mutate(func(c *[]issues_model.ExactReviewComment) { (*c)[0].Path = "a/../../b" }), "invalid inline comment"},
		{"bad side", mutate(func(c *[]issues_model.ExactReviewComment) { (*c)[0].Side = "both" }), "invalid inline comment"},
		{"zero line", mutate(func(c *[]issues_model.ExactReviewComment) { (*c)[0].Line = 0 }), "invalid inline comment"},
		{"empty body", mutate(func(c *[]issues_model.ExactReviewComment) { (*c)[0].Body = "  " }), "invalid inline comment"},
		{"duplicate key", mutate(func(c *[]issues_model.ExactReviewComment) { (*c)[1].Key = "f1" }), "duplicate inline comment key"},
		{"duplicate anchor", mutate(func(c *[]issues_model.ExactReviewComment) { (*c)[1] = dupAnchor }), "duplicate inline anchor"},
		{"too many", many, "too many inline comments"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := issues_model.ValidateExactReviewComments(tc.comments)
			require.Error(t, err)
			require.True(t, issues_model.IsErrExactReviewRefused(err), "expected exact refusal, got %v", err)
			assert.Contains(t, err.Error(), tc.reason)
		})
	}
}

// TestSubmitExactReviewInlineAtomicBundle submits one exact review with an
// added-line and a deleted-line inline comment: summary review, review
// comment and code comments share the review identity, anchors use the
// native sign convention, contents carry the client keys, and repeated
// reads return the identical rows.
func TestSubmitExactReviewInlineAtomicBundle(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 11})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.NoError(t, issue.LoadRepo(ctx))

	review, comm, inline, err := issues_model.SubmitExactReviewInline(ctx, doer, issue,
		issues_model.ReviewTypeReject, "two inline findings", exactTargetForIssue11(),
		[]issues_model.ExactReviewComment{inlineAddedComment(), inlineDeletedComment()})
	require.NoError(t, err)
	require.NotNil(t, review)
	require.NotNil(t, comm)
	require.Len(t, inline, 2)
	require.Equal(t, review.ID, comm.ReviewID)

	added, deleted := inline[0], inline[1]
	require.Equal(t, review.ID, added.ReviewID)
	require.Equal(t, review.ID, deleted.ReviewID)
	require.Equal(t, int64(4), added.Line)
	require.Equal(t, int64(-2), deleted.Line)
	assert.True(t, strings.HasPrefix(added.Content, "key:f1\n"))
	assert.True(t, strings.HasPrefix(deleted.Content, "key:f2\n"))
	assert.Contains(t, added.Content, "added-line finding: handle the new branch")

	require.Equal(t, "proposed", added.DiffSide())
	require.Equal(t, "previous", deleted.DiffSide())
	require.Equal(t, uint64(4), added.UnsignedLine())
	require.Equal(t, uint64(2), deleted.UnsignedLine())

	for range 3 {
		added.Issue = issue
		gotAdded, err := issues_model.FetchCodeConversation(ctx, added, doer)
		require.NoError(t, err)
		require.Len(t, gotAdded, 1)
		assert.Equal(t, added.ID, gotAdded[0].ID)

		deleted.Issue = issue
		gotDeleted, err := issues_model.FetchCodeConversation(ctx, deleted, doer)
		require.NoError(t, err)
		require.Len(t, gotDeleted, 1)
		assert.Equal(t, deleted.ID, gotDeleted[0].ID)
	}
}

// TestSubmitExactReviewInlineStaleHeadWritesNothing proves the exact-head
// gate refuses the whole bundle: a changed head yields "head changed" and
// no review, summary comment or code comment row survives.
func TestSubmitExactReviewInlineStaleHeadWritesNothing(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 11})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.Zero(t, countCodeComments(t))

	stale := exactTargetForIssue11()
	stale.CurrentHeadOID = "3333333333333333333333333333333333333333"
	_, _, _, err := issues_model.SubmitExactReviewInline(ctx, doer, issue,
		issues_model.ReviewTypeApprove, "stale attempt", stale,
		[]issues_model.ExactReviewComment{inlineAddedComment()})
	require.Error(t, err)
	require.True(t, issues_model.IsErrExactReviewRefused(err))
	assert.Contains(t, err.Error(), "head changed")
	assert.Zero(t, countCodeComments(t))
}

// TestSubmitExactReviewInlineJoinsOuterTransaction proves the inline
// bundle joins the caller's transaction: a forced outer rollback removes
// the review and its code comments together.
func TestSubmitExactReviewInlineJoinsOuterTransaction(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 11})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	forced := errors.New("force outer rollback")
	err := db.WithTx(db.DefaultContext, func(ctx context.Context) error {
		review, comm, inline, err := issues_model.SubmitExactReviewInline(ctx, doer, issue,
			issues_model.ReviewTypeApprove, "nested", exactTargetForIssue11(),
			[]issues_model.ExactReviewComment{inlineAddedComment()})
		require.NoError(t, err)
		require.NotZero(t, review.ID)
		require.NotZero(t, comm.ID)
		require.Len(t, inline, 1)
		return forced
	})
	require.ErrorIs(t, err, forced)

	reviews, err := issues_model.FindReviews(db.DefaultContext, issues_model.FindReviewOptions{
		IssueID: 11, ReviewerID: 2,
		Types: []issues_model.ReviewType{issues_model.ReviewTypeApprove},
	})
	require.NoError(t, err)
	assert.Empty(t, reviews)
	assert.Zero(t, countCodeComments(t))
}

// TestSubmitExactReviewInlineDuplicateAnchorWritesNothing proves a
// structurally refused bundle writes nothing at all.
func TestSubmitExactReviewInlineDuplicateAnchorWritesNothing(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 11})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	dup := inlineAddedComment()
	dup.Key = "f9"
	_, _, _, err := issues_model.SubmitExactReviewInline(ctx, doer, issue,
		issues_model.ReviewTypeApprove, "dup attempt", exactTargetForIssue11(),
		[]issues_model.ExactReviewComment{inlineAddedComment(), dup})
	require.Error(t, err)
	require.True(t, issues_model.IsErrExactReviewRefused(err))
	assert.Contains(t, err.Error(), "duplicate inline anchor")
	assert.Zero(t, countCodeComments(t))
}
