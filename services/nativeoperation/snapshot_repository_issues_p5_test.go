// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"fmt"
	"testing"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/require"
)

// P5 narrow native proof (FW29): complete actor-bound open-issue
// enumeration through the generic repository_issues family.
//
// Fifty-five open non-PR issues force a later page under the default
// 50-item bound; closed issues and pulls must stay excluded. The proof
// also covers exact receipt replay, atomic stale/oversize refusal and
// frozen dependency edges across enumeration (T34/T37 native packet
// facts). Soda acceptance/adoption assertions are FW32, not here.

func TestSnapshotRepositoryIssuesPaginatesBeyondFirstPage(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	for i := int64(0); i < 55; i++ {
		index := int64(200) + i
		insertSnapshotIssue(t, 1, index, fmt.Sprintf("Objective %d", index), "body", 0)
	}
	closed := []int64{300, 301, 302}
	for _, index := range closed {
		issue := insertSnapshotIssue(t, 1, index, fmt.Sprintf("Objective %d", index), "body", 0)
		issue.IsClosed = true
		issue.ClosedUnix = timeutil.TimeStamp(1700000300)
		_, err := db.GetEngine(ctx).ID(issue.ID).Cols("is_closed", "closed_unix").Update(issue)
		require.NoError(t, err)
	}
	for _, index := range []int64{400, 401} {
		issue := insertSnapshotIssue(t, 1, index, fmt.Sprintf("Objective %d", index), "body", 0)
		issue.IsPull = true
		_, err := db.GetEngine(ctx).ID(issue.ID).Cols("is_pull").Update(issue)
		require.NoError(t, err)
	}

	read := func(cursor string, limit int) *sdk.SnapshotRepositoryIssuesPage {
		req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyRepositoryIssues})
		req.Cursor = cursor
		req.Limit = limit
		snapshot, err := svc.ReadSnapshot(ctx, snapshotAuthority(2, req), req)
		require.NoError(t, err)
		require.NotNil(t, snapshot.RepositoryIssues)
		require.Nil(t, snapshot.Issue, "unrequested families stay absent")
		return snapshot.RepositoryIssues
	}

	// Expected open set comes from live rows so pre-existing fixture
	// issues join the assertion instead of breaking it.
	var expected []int64
	require.NoError(t, db.GetEngine(ctx).Table("issue").Cols("`index`").
		Where("repo_id = ? AND is_closed = ? AND is_pull = ?", 1, false, false).
		Asc("`index`").Find(&expected))
	require.Greater(t, len(expected), 50, "fixture must force a later page")
	require.Contains(t, expected, int64(200))
	require.Contains(t, expected, int64(254))

	first := read("", 0)
	require.False(t, first.Complete)
	require.Len(t, first.Items, 50)
	require.Equal(t, fmt.Sprint(expected[49]), first.NextCursor)
	previous := ""
	for i, item := range first.Items {
		require.True(t, item.Visible)
		require.NotEqual(t, "", item.IssueID)
		require.Greater(t, item.IssueIndex, previous)
		previous = item.IssueIndex
		require.Equal(t, fmt.Sprint(expected[i]), item.IssueIndex)
	}

	second := read(first.NextCursor, 0)
	require.True(t, second.Complete)
	require.Empty(t, second.NextCursor)
	require.Len(t, second.Items, len(expected)-50)
	for i, item := range second.Items {
		require.Equal(t, fmt.Sprint(expected[50+i]), item.IssueIndex)
	}

	seen := map[string]bool{}
	for _, page := range []*sdk.SnapshotRepositoryIssuesPage{first, second} {
		for _, item := range page.Items {
			require.False(t, seen[item.IssueIndex], "duplicate index %s", item.IssueIndex)
			seen[item.IssueIndex] = true
		}
	}
	require.Len(t, seen, len(expected))
	for _, index := range closed {
		require.False(t, seen[fmt.Sprint(index)], "closed index enumerated")
	}
	require.False(t, seen["400"], "pull index enumerated")
	require.False(t, seen["401"], "pull index enumerated")

	// A cursor past the final index is a terminal empty page, not an error.
	empty := read("99999", 0)
	require.True(t, empty.Complete)
	require.Empty(t, empty.Items)
}

func TestSnapshotRepositoryIssuesReplayIsExact(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	for i := int64(0); i < 3; i++ {
		insertSnapshotIssue(t, 1, 500+i, fmt.Sprintf("Objective %d", 500+i), "body", 0)
	}
	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyRepositoryIssues})
	first, err := svc.ReadSnapshot(ctx, snapshotAuthority(2, req), req)
	require.NoError(t, err)
	second, err := svc.ReadSnapshot(ctx, snapshotAuthority(2, req), req)
	require.NoError(t, err)
	require.Equal(t, first.RepositoryIssues, second.RepositoryIssues)
}

func TestSnapshotRepositoryIssuesRefusesAtomically(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	oversize := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyRepositoryIssues})
	oversize.Limit = 51
	_, err := svc.ReadSnapshot(ctx, snapshotAuthority(2, oversize), oversize)
	require.ErrorIs(t, err, ErrSnapshotInvalid)

	for _, cursor := range []string{"not-an-id", "0", "007"} {
		req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyRepositoryIssues})
		req.Cursor = cursor
		_, err := svc.ReadSnapshot(ctx, snapshotAuthority(2, req), req)
		require.ErrorIs(t, err, ErrSnapshotInvalid, "cursor %q", cursor)
	}

	// The family is requested alone: no selector may accompany it.
	scoped := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyRepositoryIssues})
	scoped.IssueIndex = "1"
	_, err = svc.ReadSnapshot(ctx, snapshotAuthority(2, scoped), scoped)
	require.ErrorIs(t, err, ErrSnapshotInvalid)

	// Denied enumeration refuses without leaking existence: user4 cannot
	// read user2's private repo2.
	denied := snapshotProofRequest("2", "4", []string{sdk.SnapshotFamilyRepositoryIssues})
	_, err = svc.ReadSnapshot(ctx, snapshotAuthority(4, denied), denied)
	require.ErrorIs(t, err, ErrSnapshotNotFound)

	// Enumeration claims nothing: the native revision bracket holds
	// across the full paged walk.
	before, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, before.Idle)
	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyRepositoryIssues})
	snapshot, err := svc.ReadSnapshot(ctx, snapshotAuthority(2, req), req)
	require.NoError(t, err)
	require.NotNil(t, snapshot.RepositoryIssues)
	after, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, after.Idle)
	require.Equal(t, before.Revision, after.Revision)
}

func TestSnapshotRepositoryIssuesPreservesGraphEdges(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	source := insertSnapshotIssue(t, 1, 600, "Objective", "body", 0)
	target := insertSnapshotIssue(t, 1, 601, "Objective", "body", 0)
	edge := &issues_model.IssueDependency{
		IssueID: source.ID, DependencyID: target.ID, UserID: 2,
		CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000000),
	}
	unittest.AssertSuccessfulInsert(t, edge)
	before, err := issues_model.ListIssueDependencies(ctx, source.ID)
	require.NoError(t, err)
	require.Len(t, before, 1)

	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyRepositoryIssues})
	snapshot, err := svc.ReadSnapshot(ctx, snapshotAuthority(2, req), req)
	require.NoError(t, err)
	require.True(t, snapshot.RepositoryIssues.Complete)

	after, err := issues_model.ListIssueDependencies(ctx, source.ID)
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, before[0].ID, after[0].ID, "enumeration must not disturb edge occurrences")
	require.Equal(t, target.ID, after[0].DependencyID)

	// Exact bodies still travel through the individual families under the
	// same stable revision.
	issueReq := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyIssue})
	issueReq.IssueIndex = "600"
	issueSnapshot, err := svc.ReadSnapshot(ctx, snapshotAuthority(2, issueReq), issueReq)
	require.NoError(t, err)
	require.True(t, issueSnapshot.Issue.Visible)
	require.True(t, issueSnapshot.Issue.Complete)
}
