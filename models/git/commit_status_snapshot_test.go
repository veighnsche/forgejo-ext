// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git_test

import (
	"testing"

	git_model "forgejo.org/models/git"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/require"
)

const (
	checkSnapshotSHA = "0123456789abcdef0123456789abcdef01234567"
	checkSnapshotRef = "refs/heads/candidate"
)

func snapshotStatus() *git_model.CommitStatus {
	return &git_model.CommitStatus{
		ID: 61, Index: 2, RepoID: 3, SHA: checkSnapshotSHA,
		Context: "verify.yaml", State: "success", CreatorID: 7,
		CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000050),
	}
}

func TestCheckSnapshotPreservesExactState(t *testing.T) {
	snap, err := git_model.NewCheckSnapshot(snapshotStatus(), true, "", true)
	require.NoError(t, err)
	require.True(t, snap.Visible)
	require.True(t, snap.Complete)
	require.Equal(t, checkSnapshotSHA, snap.SHA)
	require.Equal(t, "verify.yaml", snap.Context)
	require.Equal(t, "success", snap.State)

	hidden, err := git_model.NewCheckSnapshot(snapshotStatus(), false, git_model.CheckHiddenNoAccess, true)
	require.NoError(t, err)
	require.False(t, hidden.Visible)
	require.Equal(t, checkSnapshotSHA, hidden.SHA)
	require.Empty(t, hidden.Context)
	require.Empty(t, hidden.State)

	bad := snapshotStatus()
	bad.State = "skipped"
	_, err = git_model.NewCheckSnapshot(bad, true, "", true)
	require.ErrorIs(t, err, git_model.ErrCheckSnapshotInvalid, "unknown check state must refuse")
}

func TestCheckSetRequiresCompleteCommitBinding(t *testing.T) {
	one, err := git_model.NewCheckSnapshot(snapshotStatus(), true, "", true)
	require.NoError(t, err)
	set, err := git_model.NewCheckSet(3, checkSnapshotSHA, []git_model.CheckSnapshot{one}, 1, true)
	require.NoError(t, err)
	require.True(t, set.Complete)

	_, err = git_model.NewCheckSet(3, checkSnapshotSHA, []git_model.CheckSnapshot{one}, 2, true)
	require.ErrorIs(t, err, git_model.ErrCheckSnapshotInvalid, "missing check page must not report complete")

	other := snapshotStatus()
	other.SHA = "abcdef0123456789abcdef0123456789abcdef01"
	stale, err := git_model.NewCheckSnapshot(other, true, "", true)
	require.NoError(t, err)
	_, err = git_model.NewCheckSet(3, checkSnapshotSHA, []git_model.CheckSnapshot{stale}, 1, true)
	require.ErrorIs(t, err, git_model.ErrCheckSnapshotInvalid, "stale-commit check must not join this set")
}

func TestRefSnapshotBindsFullTip(t *testing.T) {
	snap, err := git_model.NewRefSnapshot(3, checkSnapshotRef, checkSnapshotSHA, true, true, "", true)
	require.NoError(t, err)
	require.True(t, snap.Exists)
	require.Equal(t, checkSnapshotSHA, snap.OID)

	missing, err := git_model.NewRefSnapshot(3, checkSnapshotRef, "", false, true, "", true)
	require.NoError(t, err)
	require.False(t, missing.Exists)

	_, err = git_model.NewRefSnapshot(3, checkSnapshotRef, "abc123", true, true, "", true)
	require.ErrorIs(t, err, git_model.ErrCheckSnapshotInvalid, "abbreviated OID must refuse")

	_, err = git_model.NewRefSnapshot(3, "candidate", checkSnapshotSHA, true, true, "", true)
	require.ErrorIs(t, err, git_model.ErrCheckSnapshotInvalid, "short ref name must refuse")

	hidden, err := git_model.NewRefSnapshot(3, checkSnapshotRef, checkSnapshotSHA, true, false, git_model.CheckHiddenNoAccess, true)
	require.NoError(t, err)
	require.False(t, hidden.Visible)
	require.False(t, hidden.Exists, "hidden ref must not leak existence")
	require.Empty(t, hidden.OID)
}
