// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"crypto/sha1"
	"fmt"
	"testing"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/require"
)

// F-read proof: permission-checked snapshots with native versions,
// creation/lifecycle evidence, dependency occurrences, refs/reviews/checks
// and completeness, bracketed by the atomic native revision. Reads run
// against live database/ref state with fixture repositories on disk.

const (
	snapshotProofSHA = "1234123412341234123412341234123412341234"
	snapshotBranch2  = "985f0301dba5e7b34be866819cd15ad3d8f508ee"
	snapshotMaster   = "65f1bf27bc3bf70f64657658635e66094edbcb4d"
)

func snapshotProofRequest(repo, actor string, families []string) sdk.SnapshotRequest {
	return sdk.SnapshotRequest{
		RepositoryID: repo,
		ActorID:      actor,
		Families:     families,
	}
}

func insertSnapshotIssue(t *testing.T, repoID, index int64, title, content string, version int) *issues_model.Issue {
	t.Helper()
	issue := &issues_model.Issue{
		RepoID:         repoID,
		Index:          index,
		PosterID:       2,
		Title:          title,
		Content:        content,
		ContentVersion: version,
		CreatedUnix:    timeutil.TimeStamp(1700000000),
		UpdatedUnix:    timeutil.TimeStamp(1700000100),
	}
	unittest.AssertSuccessfulInsert(t, issue)
	return issue
}

func insertSnapshotComment(t *testing.T, issueID int64, commentType issues_model.CommentType, content string) *issues_model.Comment {
	t.Helper()
	comment := &issues_model.Comment{
		IssueID:        issueID,
		PosterID:       2,
		Type:           commentType,
		Content:        content,
		ContentVersion: 1,
		CreatedUnix:    timeutil.TimeStamp(1700000200),
		UpdatedUnix:    timeutil.TimeStamp(1700000200),
	}
	unittest.AssertSuccessfulInsert(t, comment)
	return comment
}

func TestSnapshotReadIssueExposesVersionsAndUnverifiedProvenance(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyIssue})
	req.IssueIndex = "1"
	snapshot, err := svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	require.Equal(t, "1", snapshot.RepositoryID)
	require.NotNil(t, snapshot.Issue)
	issue := snapshot.Issue
	require.True(t, issue.Visible)
	require.True(t, issue.Complete)
	require.Equal(t, "1", issue.ID)
	require.Equal(t, "1", issue.Index)
	require.Equal(t, "issue1", issue.Title)
	require.Equal(t, "content for the first issue", issue.Content)
	require.Equal(t, 0, issue.ContentVersion)
	require.NotEmpty(t, issue.TitleDigest)
	require.NotEmpty(t, issue.ContentDigest)
	require.False(t, issue.Provenance.Verified, "fixture issue without first-created history must not verify provenance")
	require.False(t, issue.Provenance.FirstCreated)
	require.Len(t, issue.Lifecycle, 2, "fixture issue1 carries close/reopen transitions")
	require.Equal(t, "closed", issue.Lifecycle[0].Kind)
	require.Equal(t, "reopened", issue.Lifecycle[1].Kind)
	require.Nil(t, snapshot.Pull, "unrequested families stay absent")
}

func TestSnapshotReadIssueBindsProvenanceAndLifecycle(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	issue := insertSnapshotIssue(t, 1, 101, "Exact objective", "Selected body", 0)
	unittest.AssertSuccessfulInsert(t, &issues_model.ContentHistory{
		PosterID: 2, IssueID: issue.ID, CommentID: 0,
		EditedUnix: timeutil.TimeStamp(1700000000), ContentText: "Selected body",
		IsFirstCreated: true,
	})
	retitle := insertSnapshotComment(t, issue.ID, issues_model.CommentTypeChangeTitle, "")
	retitle.OldTitle = "Old objective"
	retitle.NewTitle = "Exact objective"
	_, err := db.GetEngine(ctx).ID(retitle.ID).Cols("old_title", "new_title").Update(retitle)
	require.NoError(t, err)
	insertSnapshotComment(t, issue.ID, issues_model.CommentTypeClose, "")
	insertSnapshotComment(t, issue.ID, issues_model.CommentTypeReopen, "")
	// Non-lifecycle comments never become lifecycle events.
	insertSnapshotComment(t, issue.ID, issues_model.CommentTypeComment, "discussion")

	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyIssue})
	req.IssueIndex = "101"
	snapshot, err := svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	got := snapshot.Issue
	require.True(t, got.Visible)
	require.True(t, got.Complete)
	require.True(t, got.Provenance.Verified)
	require.True(t, got.Provenance.FirstCreated)
	require.Equal(t, "2", got.Provenance.PosterID)
	require.Len(t, got.Lifecycle, 3)
	require.Equal(t, "retitled", got.Lifecycle[0].Kind)
	require.Equal(t, "Old objective", got.Lifecycle[0].OldTitle)
	require.Equal(t, "Exact objective", got.Lifecycle[0].NewTitle)
	require.Equal(t, "closed", got.Lifecycle[1].Kind)
	require.Equal(t, "reopened", got.Lifecycle[2].Kind)
}

func TestSnapshotReadDetectsEditRevertDeleteRecreate(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	issue := insertSnapshotIssue(t, 1, 102, "Objective", "alpha", 0)
	read := func(index string) *sdk.SnapshotIssue {
		req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyIssue})
		req.IssueIndex = index
		snapshot, err := svc.ReadSnapshot(ctx, 2, req)
		require.NoError(t, err)
		return snapshot.Issue
	}
	first := read("102")
	require.Equal(t, 0, first.ContentVersion)

	// Edit changes the version and digest together.
	issue.Content = "beta"
	issue.ContentVersion = 1
	issue.UpdatedUnix = timeutil.TimeStamp(1700000200)
	_, err := db.GetEngine(ctx).ID(issue.ID).Cols("content", "content_version", "updated_unix").Update(issue)
	require.NoError(t, err)
	edited := read("102")
	require.Equal(t, 1, edited.ContentVersion)
	require.NotEqual(t, first.ContentDigest, edited.ContentDigest)

	// Revert restores the digest but the version still moves: consumers
	// detect the round trip by version, never by text comparison.
	issue.Content = "alpha"
	issue.ContentVersion = 2
	_, err = db.GetEngine(ctx).ID(issue.ID).Cols("content", "content_version").Update(issue)
	require.NoError(t, err)
	reverted := read("102")
	require.Equal(t, first.ContentDigest, reverted.ContentDigest)
	require.Equal(t, 2, reverted.ContentVersion)

	// Delete removes the record; the read fails closed without leaking.
	_, err = db.GetEngine(ctx).ID(issue.ID).Delete(new(issues_model.Issue))
	require.NoError(t, err)
	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyIssue})
	req.IssueIndex = "102"
	_, err = svc.ReadSnapshot(ctx, 2, req)
	require.ErrorIs(t, err, ErrSnapshotNotFound)

	// Recreate yields a new identity, never the deleted one.
	recreated := insertSnapshotIssue(t, 1, 103, "Objective", "alpha", 0)
	require.NotEqual(t, issue.ID, recreated.ID)
	got := read("103")
	require.True(t, got.Visible)
	require.Equal(t, fmt.Sprint(recreated.ID), got.ID)
}

func TestSnapshotReadMissingAndGatedSinglesShareNotFound(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	// Missing issue index.
	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyIssue})
	req.IssueIndex = "9999"
	_, err := svc.ReadSnapshot(ctx, 2, req)
	require.ErrorIs(t, err, ErrSnapshotNotFound)

	// Missing pull request.
	req = snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyPull})
	req.PullNumber = "9999"
	_, err = svc.ReadSnapshot(ctx, 2, req)
	require.ErrorIs(t, err, ErrSnapshotNotFound)

	// Actor without repository access sees the same outcome: user4 cannot
	// read user2's private repo2.
	req = snapshotProofRequest("2", "4", []string{sdk.SnapshotFamilyIssue})
	req.IssueIndex = "1"
	_, err = svc.ReadSnapshot(ctx, 4, req)
	require.ErrorIs(t, err, ErrSnapshotNotFound)

	// Request actor must equal the verified actor.
	req = snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyIssue})
	req.IssueIndex = "1"
	_, err = svc.ReadSnapshot(ctx, 4, req)
	require.ErrorIs(t, err, ErrSnapshotInvalid)
}

func TestSnapshotReadCommentsPageWithCursorAndIDs(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	issue := insertSnapshotIssue(t, 1, 104, "Objective", "body", 0)
	first := insertSnapshotComment(t, issue.ID, issues_model.CommentTypeComment, "one")
	second := insertSnapshotComment(t, issue.ID, issues_model.CommentTypeComment, "two")
	third := insertSnapshotComment(t, issue.ID, issues_model.CommentTypeComment, "three")

	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyComments})
	req.IssueIndex = "104"
	snapshot, err := svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	page := snapshot.Comments
	require.True(t, page.Complete)
	require.Equal(t, 3, page.Total)
	require.Len(t, page.Items, 3)
	require.Equal(t, fmt.Sprint(first.ID), page.Items[0].ID)
	require.Equal(t, "comment", page.Items[0].Type)
	require.NotEmpty(t, page.Items[0].ContentDigest)

	// Cursor paging selects strictly greater IDs.
	req.Cursor = fmt.Sprint(first.ID)
	snapshot, err = svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	require.True(t, snapshot.Comments.Complete)
	require.Equal(t, 2, snapshot.Comments.Total)
	require.Equal(t, fmt.Sprint(second.ID), snapshot.Comments.Items[0].ID)
	require.Equal(t, fmt.Sprint(third.ID), snapshot.Comments.Items[1].ID)

	// A limit below the total reports incomplete, never a silent prefix.
	req.Cursor = ""
	req.Limit = 2
	snapshot, err = svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	require.False(t, snapshot.Comments.Complete)
	require.Equal(t, 3, snapshot.Comments.Total)
	require.Len(t, snapshot.Comments.Items, 2)

	// Explicit IDs select across the page; unknown IDs fail closed.
	req = snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyComments})
	req.CommentIDs = []string{fmt.Sprint(first.ID), fmt.Sprint(third.ID)}
	snapshot, err = svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	require.True(t, snapshot.Comments.Complete)
	require.Equal(t, 2, snapshot.Comments.Total)

	req.CommentIDs = []string{"999999"}
	_, err = svc.ReadSnapshot(ctx, 2, req)
	require.ErrorIs(t, err, ErrSnapshotNotFound)

	// IDs spanning two issues are ambiguous and refuse.
	other := insertSnapshotIssue(t, 1, 105, "Other", "body", 0)
	foreign := insertSnapshotComment(t, other.ID, issues_model.CommentTypeComment, "foreign")
	req.CommentIDs = []string{fmt.Sprint(first.ID), fmt.Sprint(foreign.ID)}
	_, err = svc.ReadSnapshot(ctx, 2, req)
	require.ErrorIs(t, err, ErrSnapshotInvalid)
	_ = second
}

func TestSnapshotReadDependenciesRedactsHiddenTarget(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	// user4 reads repo3 but cannot read user2's private repo2. Edge to
	// repo2's issue4 hides its target; edge to repo3's issue12 stays
	// visible. Both occurrences stay attributable.
	hidden := &issues_model.IssueDependency{IssueID: 6, DependencyID: 4, UserID: 4,
		CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000000)}
	unittest.AssertSuccessfulInsert(t, hidden)
	shown := &issues_model.IssueDependency{IssueID: 6, DependencyID: 12, UserID: 4,
		CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000000)}
	unittest.AssertSuccessfulInsert(t, shown)

	svc := NewService()
	req := snapshotProofRequest("3", "4", []string{sdk.SnapshotFamilyDependencies})
	req.IssueIndex = "1"
	snapshot, err := svc.ReadSnapshot(ctx, 4, req)
	require.NoError(t, err)
	page := snapshot.Dependencies
	require.True(t, page.Complete)
	require.Equal(t, 2, page.Total)
	require.Len(t, page.Items, 2)

	first, second := page.Items[0], page.Items[1]
	require.False(t, first.Visible)
	require.Equal(t, "no_access", first.HiddenReason)
	require.Equal(t, fmt.Sprint(hidden.ID), first.OccurrenceID)
	require.Empty(t, first.DependencyID, "hidden edge must not leak the target")
	require.True(t, first.Complete)
	require.True(t, second.Visible)
	require.Equal(t, fmt.Sprint(shown.ID), second.OccurrenceID)
	require.Equal(t, "12", second.DependencyID)

	// Removal plus readdition yields a new occurrence ID.
	_, err = db.GetEngine(ctx).ID(shown.ID).Delete(new(issues_model.IssueDependency))
	require.NoError(t, err)
	readded := &issues_model.IssueDependency{IssueID: 6, DependencyID: 12, UserID: 4,
		CreatedUnix: timeutil.TimeStamp(1700000001), UpdatedUnix: timeutil.TimeStamp(1700000001)}
	unittest.AssertSuccessfulInsert(t, readded)
	require.NotEqual(t, shown.ID, readded.ID)
	snapshot, err = svc.ReadSnapshot(ctx, 4, req)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprint(readded.ID), snapshot.Dependencies.Items[1].OccurrenceID)
}

func TestSnapshotReadPullResolvesLiveHeadTip(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	// Fixture PR index 3 is open with head branch2 on repo1; the tip comes
	// from live ref state, not the pull row.
	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyPull})
	req.PullNumber = "3"
	snapshot, err := svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	pull := snapshot.Pull
	require.True(t, pull.Visible)
	require.True(t, pull.Complete)
	require.Equal(t, "2", pull.ID)
	require.Equal(t, "3", pull.Number)
	require.Equal(t, "branch2", pull.HeadBranch)
	require.Equal(t, snapshotBranch2, pull.HeadTip)
	require.Equal(t, "master", pull.BaseBranch)
	require.False(t, pull.HasMerged)
}

func TestSnapshotReadReviewsExposeExactCandidates(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	issue := insertSnapshotIssue(t, 1, 106, "Objective", "body", 0)
	approve := &issues_model.Review{
		IssueID: issue.ID, ReviewerID: 2, Type: issues_model.ReviewTypeApprove,
		CommitID: snapshotBranch2, Content: "looks good", Official: true,
		CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000100),
	}
	unittest.AssertSuccessfulInsert(t, approve)
	comment := &issues_model.Review{
		IssueID: issue.ID, ReviewerID: 4, Type: issues_model.ReviewTypeComment,
		CommitID: snapshotMaster, Content: "note", Stale: true,
		CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000100),
	}
	unittest.AssertSuccessfulInsert(t, comment)

	svc := NewService()
	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyReviews})
	req.IssueIndex = "106"
	snapshot, err := svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	page := snapshot.Reviews
	require.True(t, page.Complete)
	require.Equal(t, 2, page.Total)
	require.Len(t, page.Items, 2)
	require.Equal(t, "APPROVED", page.Items[0].Type)
	require.Equal(t, snapshotBranch2, page.Items[0].CommitID)
	require.True(t, page.Items[0].Official)
	require.NotEmpty(t, page.Items[0].ContentDigest)
	require.Equal(t, "COMMENT", page.Items[1].Type)
	require.True(t, page.Items[1].Stale)
}

func TestSnapshotReadChecksReturnLatestPerContext(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	sha := "aaaaaaaabbbbccccddddeeeeffff000011112222"
	insertStatus := func(index int64, context, state string) {
		status := &git_model.CommitStatus{
			RepoID: 1, Index: index, SHA: sha, State: structs.CommitStatusState(state),
			Context: context, ContextHash: fmt.Sprintf("%x", sha1.Sum([]byte(context))),
			CreatorID:   2,
			CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000100),
		}
		unittest.AssertSuccessfulInsert(t, status)
	}
	insertStatus(1, "ci/build", "pending")
	insertStatus(2, "ci/build", "success")
	insertStatus(3, "lint/diff", "failure")

	svc := NewService()
	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyChecks})
	req.SHA = sha
	snapshot, err := svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	set := snapshot.Checks
	require.True(t, set.Complete)
	require.Equal(t, 2, set.Total)
	require.Len(t, set.Items, 2)
	byContext := map[string]sdk.SnapshotCheck{}
	for _, item := range set.Items {
		byContext[item.Context] = item
		require.True(t, item.Visible)
		require.True(t, item.Complete)
	}
	require.Equal(t, "success", byContext["ci/build"].State)
	require.Equal(t, int64(2), byContext["ci/build"].Index)
	require.Equal(t, "failure", byContext["lint/diff"].State)

	// An unknown commit honestly reports no checks.
	req.SHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	snapshot, err = svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	require.True(t, snapshot.Checks.Complete)
	require.Equal(t, 0, snapshot.Checks.Total)
}

func TestSnapshotReadRefsReportLiveTips(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyRefs})
	req.Refs = []string{"refs/heads/master", "refs/heads/does-not-exist"}
	snapshot, err := svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	require.Len(t, snapshot.Refs, 2)
	require.Equal(t, "refs/heads/master", snapshot.Refs[0].Ref)
	require.True(t, snapshot.Refs[0].Exists)
	require.Equal(t, snapshotMaster, snapshot.Refs[0].OID)
	require.True(t, snapshot.Refs[0].Visible)
	require.True(t, snapshot.Refs[0].Complete)
	require.False(t, snapshot.Refs[1].Exists)
	require.Empty(t, snapshot.Refs[1].OID)
	require.True(t, snapshot.Refs[1].Visible)
}

func TestSnapshotReadBracketRejectsInterferingWriter(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	before, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, before.Idle)
	require.GreaterOrEqual(t, before.Revision, int64(1))

	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyIssue})
	req.IssueIndex = "1"
	_, err = svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)

	after, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, after.Idle)
	require.Equal(t, before.Revision, after.Revision, "reads claim nothing, so the bracket holds")

	// A held ordinary writer makes the revision busy; reads still succeed
	// because F-read claims nothing, but the bracket must be rejected.
	owner := "ord:snapshot-proof/1/read"
	_, err = model.ClaimOrdinary(ctx, owner, `{"test":"snapshot-proof"}`, "snapshot-proof")
	require.NoError(t, err)
	held, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.False(t, held.Idle)
	require.Equal(t, before.Revision+1, held.Revision)
	_, err = svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err, "reads never contend with a held writer")
	require.NoError(t, model.ReleaseOwner(ctx, owner))

	moved, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, moved.Idle)
	require.Equal(t, before.Revision+1, moved.Revision)
	require.NotEqual(t, before.Revision, moved.Revision, "an intervening writer invalidates the bracket")
}
