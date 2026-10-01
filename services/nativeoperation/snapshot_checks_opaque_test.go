// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"crypto/sha1"
	"fmt"
	"testing"

	sdk "forgejo.org/extension-sdk"
	git_model "forgejo.org/models/git"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/require"
)

// Checks-family opaque read proof (checksfix-503): snapshots convey every
// stored commit-status row. Actions-posted rows carry the system Actions
// creator (no real user) and ordinary REST stores states beyond the five
// documented ones; both classes must read back through the snapshot instead
// of refusing with 503. Consumers fail closed on non-success states.

func insertOpaqueCheckRow(t *testing.T, sha string, index int64, context, state string, creatorID int64) {
	t.Helper()
	status := &git_model.CommitStatus{
		RepoID: 1, Index: index, SHA: sha, State: structs.CommitStatusState(state),
		Context: context, ContextHash: fmt.Sprintf("%x", sha1.Sum([]byte(context))),
		CreatorID:   creatorID,
		CreatedUnix: timeutil.TimeStamp(1700000000), UpdatedUnix: timeutil.TimeStamp(1700000100),
	}
	unittest.AssertSuccessfulInsert(t, status)
}

func readOpaqueChecks(t *testing.T, sha string) *sdk.SnapshotCheckSet {
	t.Helper()
	svc := NewService()
	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyChecks})
	req.SHA = sha
	snapshot, err := svc.ReadSnapshot(t.Context(), 2, req)
	require.NoError(t, err, "stored check rows must read back, never 503")
	require.NotNil(t, snapshot.Checks)
	require.True(t, snapshot.Checks.Complete)
	return snapshot.Checks
}

func TestSnapshotReadChecksConveyActionsPostedRow(t *testing.T) {
	unittest.PrepareTestEnv(t)

	sha := "cccccccccccccccccccccccccccccccccccccccc"
	insertOpaqueCheckRow(t, sha, 1, "st11-checks / assess", "pending", user_model.ActionsUserID)

	set := readOpaqueChecks(t, sha)
	require.Equal(t, 1, set.Total)
	require.Len(t, set.Items, 1)
	item := set.Items[0]
	require.True(t, item.Visible)
	require.True(t, item.Complete)
	require.Equal(t, "st11-checks / assess", item.Context)
	require.Equal(t, "pending", item.State)
	require.Equal(t, int64(1), item.Index)
	require.Empty(t, item.CreatorID, "system creator renders empty, matching the REST rendering of the same row")
}

func TestSnapshotReadChecksPassStoredStatesThrough(t *testing.T) {
	unittest.PrepareTestEnv(t)

	sha := "dddddddddddddddddddddddddddddddddddddddd"
	insertOpaqueCheckRow(t, sha, 1, "ci/cancelled", "cancelled", 2)
	insertOpaqueCheckRow(t, sha, 2, "ci/skipped", "skipped", 2)
	insertOpaqueCheckRow(t, sha, 3, "ci/custom", "awaiting-maintainer-signal", 2)

	set := readOpaqueChecks(t, sha)
	require.Equal(t, 3, set.Total)
	require.Len(t, set.Items, 3)
	byContext := map[string]sdk.SnapshotCheck{}
	for _, item := range set.Items {
		byContext[item.Context] = item
		require.True(t, item.Visible)
		require.True(t, item.Complete)
		require.Equal(t, "2", item.CreatorID)
	}
	require.Equal(t, "cancelled", byContext["ci/cancelled"].State)
	require.Equal(t, "skipped", byContext["ci/skipped"].State)
	require.Equal(t, "awaiting-maintainer-signal", byContext["ci/custom"].State)
}
