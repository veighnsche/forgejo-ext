// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues_test

import (
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/require"
)

const (
	snapshotHeadOID = "0123456789abcdef0123456789abcdef01234567"
	snapshotBaseOID = "abcdef0123456789abcdef0123456789abcdef01"
)

func snapshotIssue() *issues_model.Issue {
	return &issues_model.Issue{
		ID:             11,
		RepoID:         3,
		Index:          5,
		PosterID:       7,
		Title:          "Exact objective",
		Content:        "Selected body",
		ContentVersion: 4,
		NumComments:    2,
		CreatedUnix:    timeutil.TimeStamp(1700000000),
		UpdatedUnix:    timeutil.TimeStamp(1700000100),
	}
}

func TestIssueSnapshotExposesExactVersionsAndProvenance(t *testing.T) {
	lifecycle := []issues_model.TitleLifecycleEvent{
		{Kind: issues_model.LifecycleRetitled, AtUnix: 1700000050, ActorID: 7, OldTitle: "Old", NewTitle: "Exact objective"},
	}
	snap, err := issues_model.NewIssueSnapshot(snapshotIssue(), true, lifecycle, true, "", true)
	require.NoError(t, err)
	require.True(t, snap.Visible)
	require.True(t, snap.Complete)
	require.Equal(t, int64(11), snap.IssueID)
	require.Equal(t, 4, snap.ContentVersion)
	require.Equal(t, "Exact objective", snap.Title)
	require.Equal(t, issues_model.ContentDigest("Exact objective"), snap.TitleDigest)
	require.Equal(t, issues_model.ContentDigest("Selected body"), snap.ContentDigest)
	require.True(t, snap.Provenance.Verified)
	require.True(t, snap.Provenance.FirstCreated)
	require.Equal(t, int64(7), snap.Provenance.PosterID)
	require.Len(t, snap.Lifecycle, 1)
	require.Equal(t, issues_model.LifecycleRetitled, snap.Lifecycle[0].Kind)

	unverified, err := issues_model.NewIssueSnapshot(snapshotIssue(), false, nil, true, "", true)
	require.NoError(t, err)
	require.False(t, unverified.Provenance.Verified, "cached text without first-created history must not verify provenance")
}

func TestIssueSnapshotRedactsHiddenRecords(t *testing.T) {
	snap, err := issues_model.NewIssueSnapshot(snapshotIssue(), true, nil, false, issues_model.HiddenReasonNoAccess, true)
	require.NoError(t, err)
	require.False(t, snap.Visible)
	require.Equal(t, issues_model.HiddenReasonNoAccess, snap.HiddenReason)
	require.Equal(t, int64(11), snap.IssueID)
	require.Empty(t, snap.Title)
	require.Empty(t, snap.Content)
	require.Empty(t, snap.TitleDigest)
	require.Empty(t, snap.ContentDigest)
	require.Zero(t, snap.ContentVersion)
	require.False(t, snap.Provenance.Verified)

	_, err = issues_model.NewIssueSnapshot(snapshotIssue(), true, nil, false, "bogus", true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid)
}

func TestIssueSnapshotRefusesMalformedInput(t *testing.T) {
	_, err := issues_model.NewIssueSnapshot(nil, true, nil, true, "", true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid)

	bad := snapshotIssue()
	bad.UpdatedUnix = bad.CreatedUnix - 1
	_, err = issues_model.NewIssueSnapshot(bad, true, nil, true, "", true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid)

	closed := snapshotIssue()
	closed.IsClosed = true
	_, err = issues_model.NewIssueSnapshot(closed, true, nil, true, "", true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid, "closed flag without close timestamp must refuse")
}

func TestLifecycleEventFromCommentMapsTitleTransitions(t *testing.T) {
	retitle := &issues_model.Comment{
		ID: 21, IssueID: 11, Type: issues_model.CommentTypeChangeTitle,
		PosterID: 7, OldTitle: "Old", NewTitle: "New",
		CreatedUnix: timeutil.TimeStamp(1700000050),
	}
	event, ok := issues_model.LifecycleEventFromComment(retitle)
	require.True(t, ok)
	require.Equal(t, issues_model.LifecycleRetitled, event.Kind)
	require.Equal(t, "Old", event.OldTitle)

	plain := &issues_model.Comment{ID: 22, IssueID: 11, Type: issues_model.CommentTypeComment, CreatedUnix: timeutil.TimeStamp(1700000060)}
	_, ok = issues_model.LifecycleEventFromComment(plain)
	require.False(t, ok)
}

func TestCommentSnapshotPreservesVersions(t *testing.T) {
	comment := &issues_model.Comment{
		ID: 21, IssueID: 11, Type: issues_model.CommentTypeComment,
		PosterID: 7, Content: "answer", ContentVersion: 2,
		CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000020),
	}
	snap, err := issues_model.NewCommentSnapshot(comment, true, "", true)
	require.NoError(t, err)
	require.Equal(t, "comment", snap.Type)
	require.Equal(t, 2, snap.ContentVersion)
	require.Equal(t, issues_model.ContentDigest("answer"), snap.ContentDigest)

	hidden, err := issues_model.NewCommentSnapshot(comment, false, issues_model.HiddenReasonRedacted, true)
	require.NoError(t, err)
	require.False(t, hidden.Visible)
	require.Empty(t, hidden.Content)
	require.Empty(t, hidden.Type)

	page, err := issues_model.NewCommentPage(11, []issues_model.CommentSnapshot{snap}, 1, true)
	require.NoError(t, err)
	require.True(t, page.Complete)

	_, err = issues_model.NewCommentPage(11, []issues_model.CommentSnapshot{snap}, 2, true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid, "missing page items must not report complete")
}

func TestDependencySnapshotTracksEdgeOccurrences(t *testing.T) {
	first := &issues_model.IssueDependency{
		ID: 31, IssueID: 11, DependencyID: 12, UserID: 7,
		CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000000),
	}
	readded := &issues_model.IssueDependency{
		ID: 33, IssueID: 11, DependencyID: 12, UserID: 7,
		CreatedUnix: timeutil.TimeStamp(1700000200), UpdatedUnix: timeutil.TimeStamp(1700000200),
	}
	one, err := issues_model.NewDependencySnapshot(first, true, "", true)
	require.NoError(t, err)
	two, err := issues_model.NewDependencySnapshot(readded, true, "", true)
	require.NoError(t, err)
	require.NotEqual(t, one.OccurrenceID, two.OccurrenceID, "removal plus readdition must be a new occurrence")

	hidden, err := issues_model.NewDependencySnapshot(first, false, issues_model.HiddenReasonNoAccess, true)
	require.NoError(t, err)
	require.False(t, hidden.Visible)
	require.Equal(t, int64(31), hidden.OccurrenceID)
	require.Equal(t, int64(11), hidden.IssueID)
	require.Zero(t, hidden.DependencyID, "hidden prerequisite must not leak its target")
}

func TestPullSnapshotRequiresExactRefs(t *testing.T) {
	pr := &issues_model.PullRequest{
		ID: 41, IssueID: 11, Index: 5, HeadRepoID: 3, BaseRepoID: 3,
		HeadBranch: "candidate", BaseBranch: "main",
		MergeBase: snapshotBaseOID, Flow: issues_model.PullRequestFlowGithub,
		Status: issues_model.PullRequestStatusMergeable,
	}
	snap, err := issues_model.NewPullSnapshot(pr, snapshotHeadOID, true, "", true)
	require.NoError(t, err)
	require.Equal(t, snapshotHeadOID, snap.HeadTip)
	require.Equal(t, snapshotBaseOID, snap.MergeBase)
	require.Equal(t, "github", snap.Flow)

	_, err = issues_model.NewPullSnapshot(pr, "abc123", true, "", true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid, "abbreviated OID must refuse")

	_, err = issues_model.NewPullSnapshot(pr, "", true, "", true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid, "open PR without current tip must refuse")
}

func TestReviewSnapshotBindsExactCommit(t *testing.T) {
	review := &issues_model.Review{
		ID: 51, IssueID: 11, Type: issues_model.ReviewTypeApprove,
		ReviewerID: 9, CommitID: snapshotHeadOID, Official: true,
		CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000010),
	}
	snap, err := issues_model.NewReviewSnapshot(review, true, "", true)
	require.NoError(t, err)
	require.Equal(t, "APPROVED", snap.Type)
	require.Equal(t, snapshotHeadOID, snap.CommitID)
	require.True(t, snap.Official)
	require.False(t, snap.Stale)

	stale := *review
	stale.Stale = true
	staleSnap, err := issues_model.NewReviewSnapshot(&stale, true, "", true)
	require.NoError(t, err)
	require.True(t, staleSnap.Stale)

	bad := *review
	bad.CommitID = "short"
	_, err = issues_model.NewReviewSnapshot(&bad, true, "", true)
	require.ErrorIs(t, err, issues_model.ErrSnapshotInvalid)
}
