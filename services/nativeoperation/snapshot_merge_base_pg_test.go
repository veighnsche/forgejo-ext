// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "forgejo.org/extension-sdk"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"

	"github.com/stretchr/testify/require"
)

// SUPERVISOR-047 producer proof: SnapshotPull.MergeBase is a genuinely
// derived CURRENT merge-base from the live HeadTip and the current named
// BaseBranch tip, read through the actual Service.ReadSnapshot. The stored
// pull-row field is never a fallback. Runs on disposable PG
// (FORGEJO_TEST_PG_DSN) against the real fixture git repository.

func snapshotMergeBasePull(t *testing.T, pullNumber string) *sdk.SnapshotPull {
	t.Helper()
	svc := NewService()
	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyPull})
	req.PullNumber = pullNumber
	snapshot, err := svc.ReadSnapshot(t.Context(), snapshotAuthority(2, req), req)
	require.NoError(t, err)
	require.NotNil(t, snapshot.Pull)
	return snapshot.Pull
}

func TestSnapshotPullMergeBaseProducerOnPostgres(t *testing.T) {
	useEnrolledAuthorityPostgres(t)
	t.Run("derives current merge-base", testSnapshotPullDerivesCurrentMergeBase)
	t.Run("base is not merge-base", testSnapshotPullBaseIsNotMergeBase)
	t.Run("missing object refuses without writes", testSnapshotPullMissingObjectRefusesWithoutWrites)
	t.Run("unrelated histories refuse", testSnapshotPullUnrelatedHistoriesRefuse)
	t.Run("merged omits base preserves closure", testSnapshotPullMergedOmitsMergeBasePreservesClosure)
	t.Run("deleted head stays hidden", testSnapshotPullDeletedHeadStaysHidden)
}

func testSnapshotPullDerivesCurrentMergeBase(t *testing.T) {
	unittest.PrepareTestEnv(t)

	// Fixture PR 3 (repo1 branch2 onto master) carries a stale stored
	// merge-base from PR creation time; the live tips yield another one.
	pull := snapshotMergeBasePull(t, "3")
	require.True(t, pull.Visible)
	require.True(t, pull.Complete)
	require.Equal(t, "3", pull.Number)
	require.Equal(t, "branch2", pull.HeadBranch)
	require.Equal(t, snapshotBranch2, pull.HeadTip)
	require.Equal(t, "master", pull.BaseBranch)
	require.False(t, pull.HasMerged)

	repository := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	oracle := gitMergeBase(t, repository.RepoPath(), snapshotMaster, snapshotBranch2)
	require.NotEmpty(t, pull.MergeBase)
	require.True(t, validOID(pull.MergeBase))
	require.Equal(t, oracle, strings.ToLower(pull.MergeBase), "returned MergeBase equals the git-derived current base")
	require.NotEqual(t, "4a357436d925b5c974181ff12a994538ddc5a269", strings.ToLower(pull.MergeBase), "stale cached pull-row merge-base must not surface")
}

func testSnapshotPullBaseIsNotMergeBase(t *testing.T) {
	unittest.PrepareTestEnv(t)

	repository := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	repoPath := repository.RepoPath()
	svc := NewService()
	head, err := svc.readRef(t.Context(), repoPath, git.BranchPrefix+"branch2")
	require.NoError(t, err)
	base, err := svc.readRef(t.Context(), repoPath, git.BranchPrefix+"master")
	require.NoError(t, err)

	advanced := gitAdvanceBranch(t, repoPath, "master", base, "advance base past the merge-base", false)
	oracle := gitMergeBase(t, repoPath, advanced, head)
	require.Equal(t, base, oracle)
	require.NotEqual(t, advanced, oracle)

	pull := snapshotMergeBasePull(t, "3")
	require.True(t, pull.Visible)
	require.Equal(t, snapshotBranch2, pull.HeadTip)
	require.Equal(t, oracle, strings.ToLower(pull.MergeBase), "returned MergeBase follows the live pair, not the advanced base")
	require.NotEqual(t, advanced, strings.ToLower(pull.MergeBase), "the base is not itself the merge-base")
}

func testSnapshotPullMissingObjectRefusesWithoutWrites(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	repository := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	repoPath := repository.RepoPath()
	svc := NewService()
	base, err := svc.readRef(ctx, repoPath, git.BranchPrefix+"master")
	require.NoError(t, err)

	// Mint an orphan head commit, then remove its loose object: the ref
	// still resolves, but the current pair is unverifiable, so the read
	// refuses instead of binding.
	orphan := gitAdvanceBranch(t, repoPath, "branch2", base, "head without backing object", true)
	objectPath := filepath.Join(repoPath, "objects", orphan[:2], orphan[2:])
	require.FileExists(t, objectPath)
	require.NoError(t, os.Remove(objectPath))

	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyPull})
	req.PullNumber = "3"
	_, err = svc.ReadSnapshot(ctx, snapshotAuthority(2, req), req)
	require.ErrorIs(t, err, ErrSnapshotUnavailable)

	tip, err := svc.readRef(ctx, repoPath, git.BranchPrefix+"branch2")
	require.NoError(t, err)
	require.Equal(t, orphan, strings.ToLower(tip), "a refused read writes nothing")
}

func testSnapshotPullUnrelatedHistoriesRefuse(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	repository := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	repoPath := repository.RepoPath()
	svc := NewService()
	base, err := svc.readRef(ctx, repoPath, git.BranchPrefix+"master")
	require.NoError(t, err)

	// An orphan base shares no history with the head: merge-base has no
	// answer, so the read refuses instead of certifying either tip.
	orphan := gitAdvanceBranch(t, repoPath, "master", base, "orphan base without common history", true)

	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyPull})
	req.PullNumber = "3"
	_, err = svc.ReadSnapshot(ctx, snapshotAuthority(2, req), req)
	require.ErrorIs(t, err, ErrSnapshotUnavailable)

	tip, err := svc.readRef(ctx, repoPath, git.BranchPrefix+"master")
	require.NoError(t, err)
	require.Equal(t, orphan, strings.ToLower(tip), "a refused read writes nothing")
}

func testSnapshotPullMergedOmitsMergeBasePreservesClosure(t *testing.T) {
	unittest.PrepareTestEnv(t)

	// Fixture PR 2 is merged with its head branch gone: historical
	// completion evidence stays, but no current pair exists, so no fresh
	// MergeBase is certified.
	pull := snapshotMergeBasePull(t, "2")
	require.True(t, pull.Visible)
	require.True(t, pull.Complete)
	require.True(t, pull.HasMerged)
	require.Equal(t, "1a8823cd1a9549fde083f992f6b9b87a7ab74fb3", strings.ToLower(pull.MergedCommit))
	require.Equal(t, "2", pull.MergerID)
	require.Empty(t, pull.MergeBase, "merged history omits MergeBase rather than certifying the cached row")
}

func testSnapshotPullDeletedHeadStaysHidden(t *testing.T) {
	unittest.PrepareTestEnv(t)

	repository := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	gitRun(t, repository.RepoPath(), nil, "update-ref", "-d", git.BranchPrefix+"branch2")

	// The open pull is readable but its head tip is gone: the existing
	// hidden tolerance holds, binding no OID at all.
	pull := snapshotMergeBasePull(t, "3")
	require.False(t, pull.Visible)
	require.Equal(t, "no_access", pull.HiddenReason)
	require.True(t, pull.Complete)
	require.Empty(t, pull.MergeBase)
}
