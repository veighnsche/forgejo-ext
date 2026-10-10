// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"strings"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	perm_model "forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"

	"github.com/stretchr/testify/require"
)

func TestSnapshotAncestryUsesSelectedRepositoryCommitGraph(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	repository, err := repo_model.GetRepositoryByID(ctx, 1)
	require.NoError(t, err)
	user, err := user_model.GetUserByID(ctx, 2)
	require.NoError(t, err)
	permission, err := access_model.GetUserRepoPermission(ctx, repository, user)
	require.NoError(t, err)
	loader := &snapshotLoader{service: NewService(), repository: repository, user: user, permission: permission}

	serviceRequest := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyRefs, sdk.SnapshotFamilyAncestry})
	serviceRequest.Refs = []string{"refs/heads/master", "refs/heads/branch2"}
	serviceRequest.Ancestry = &sdk.SnapshotAncestryRequest{AncestorOID: snapshotMaster, DescendantOID: snapshotBranch2}
	serviceSnapshot, err := NewService().ReadSnapshot(ctx, 2, serviceRequest)
	require.NoError(t, err)
	require.NotNil(t, serviceSnapshot.Ancestry)
	require.Equal(t, snapshotMaster, serviceSnapshot.Ancestry.AncestorOID)
	require.Equal(t, snapshotBranch2, serviceSnapshot.Ancestry.DescendantOID)
	require.True(t, serviceSnapshot.Ancestry.Reachable)
	require.Len(t, serviceSnapshot.Refs, 2)
	require.True(t, serviceSnapshot.Refs[0].Visible && serviceSnapshot.Refs[0].Exists)
	require.True(t, serviceSnapshot.Refs[1].Visible && serviceSnapshot.Refs[1].Exists)

	request := func(ancestor, descendant string) *sdk.SnapshotAncestryRequest {
		return &sdk.SnapshotAncestryRequest{AncestorOID: ancestor, DescendantOID: descendant}
	}
	for _, tc := range []struct {
		name, ancestor, descendant string
		reachable                  bool
	}{
		{name: "equal commit", ancestor: snapshotMaster, descendant: snapshotMaster, reachable: true},
		{name: "transitive ancestor", ancestor: snapshotMaster, descendant: snapshotBranch2, reachable: true},
		{name: "unrelated histories", ancestor: "4a357436d925b5c974181ff12a994538ddc5a269", descendant: snapshotBranch2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := loader.ancestry(ctx, request(tc.ancestor, tc.descendant))
			require.NoError(t, err)
			require.Equal(t, tc.ancestor, got.AncestorOID)
			require.Equal(t, tc.descendant, got.DescendantOID)
			require.Equal(t, tc.reachable, got.Reachable)
		})
	}

	gitRepo, err := git.OpenRepository(ctx, repository.RepoPath())
	require.NoError(t, err)
	commit, err := gitRepo.GetCommit(snapshotMaster)
	require.NoError(t, err)
	const tagName = "snapshot-ancestry-test"
	identity := []string{
		"GIT_AUTHOR_NAME=Snapshot ancestry test", "GIT_AUTHOR_EMAIL=snapshot-ancestry@example.invalid",
		"GIT_COMMITTER_NAME=Snapshot ancestry test", "GIT_COMMITTER_EMAIL=snapshot-ancestry@example.invalid",
	}
	require.NoError(t, gitRepo.CreateAnnotatedTagWithEnv(tagName, "ancestry selector regression", snapshotMaster, identity))
	tagOID, err := gitRepo.GetTagID(tagName)
	require.NoError(t, err)
	require.NoError(t, gitRepo.Close())

	for _, tc := range []struct {
		name, ancestor, descendant string
	}{
		{name: "annotated tag as ancestor selector", ancestor: tagOID, descendant: snapshotBranch2},
		{name: "annotated tag as descendant selector", ancestor: snapshotMaster, descendant: tagOID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loader.ancestry(ctx, request(tc.ancestor, tc.descendant))
			require.ErrorIs(t, err, ErrSnapshotUnavailable)
		})
	}

	for _, tc := range []struct {
		name, ancestor string
	}{
		{name: "missing commit", ancestor: strings.Repeat("0", 40)},
		{name: "non-commit object", ancestor: commit.Tree.ID.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loader.ancestry(ctx, request(tc.ancestor, snapshotBranch2))
			require.ErrorIs(t, err, ErrSnapshotUnavailable)
		})
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = loader.ancestry(cancelled, request(snapshotMaster, snapshotBranch2))
	require.ErrorIs(t, err, ErrSnapshotUnavailable)

	expired, expire := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer expire()
	_, err = loader.ancestry(expired, request(snapshotMaster, snapshotBranch2))
	require.ErrorIs(t, err, ErrSnapshotUnavailable)

	permission.UnitsMode = map[unit.Type]perm_model.AccessMode{unit.TypeCode: perm_model.AccessModeNone}
	loader.permission = permission
	_, err = loader.ancestry(ctx, request(snapshotMaster, snapshotBranch2))
	require.ErrorIs(t, err, ErrSnapshotNotFound)
}
