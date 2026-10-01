// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"os"
	"testing"

	git_model "forgejo.org/models/git"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/require"
)

func useIsolatedAppData(t *testing.T) {
	t.Helper()
	previous := setting.AppDataPath
	setting.AppDataPath = t.TempDir()
	t.Cleanup(func() { setting.AppDataPath = previous })
}

func inhibitDomain(t *testing.T) {
	t.Helper()
	require.NoError(t, os.WriteFile(model.OfflineMarkerPath(), []byte("recovery\n"), 0o600))
}

func claimOrdinaryOwner(t *testing.T, ctx context.Context, owner string, scope Scope) *model.Reservation {
	t.Helper()
	encoded, err := encodeScope(scope)
	require.NoError(t, err)
	claimed, err := model.ClaimOrdinary(ctx, owner, encoded, "verifier")
	require.NoError(t, err)
	return claimed
}

func TestRecoverRequiresInhibition(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	_, err := svc.Recover(t.Context(), "ord:branch/1/x", 1)
	require.ErrorIs(t, err, ErrNotInhibited)
}

func TestRecoverIdle(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	inhibitDomain(t)
	svc := admissionService(t, nil, 0)

	assessment, err := svc.Recover(t.Context(), "ord:branch/1/x", 1)
	require.NoError(t, err)
	require.Equal(t, RecoveryIdle, assessment.Verdict)
}

func TestRecoverWrongOwnerAndGeneration(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:branch-delete/1/branch2/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		RepositoryID: 1,
		Family:       FamilyBranchDelete,
	})
	inhibitDomain(t)

	wrongOwner, err := svc.Recover(ctx, "ord:branch-delete/1/other/nonce", claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, wrongOwner.Verdict)
	require.Equal(t, model.ReasonWrongOwner, wrongOwner.Reason)

	wrongGeneration, err := svc.Recover(ctx, claimed.Owner, claimed.Generation+1)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, wrongGeneration.Verdict)
	require.Equal(t, ReasonRecoveryWrongGeneration, wrongGeneration.Reason)

	held, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, claimed.Owner, held.Owner)
}

func TestRecoverUnknownFamily(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, nil, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:future/1/x/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		RepositoryID: 1,
		Family:       "future-family",
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnknownFamily, assessment.Reason)

	held, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, claimed.Owner, held.Owner)
}

func TestRecoverBranchDeleteKnownDeleted(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	capabilityDir := t.TempDir()
	svc := &Service{refs: stubRefs{err: git.ErrNotExist{ID: "refs/heads/branch2"}}, capabilityDir: capabilityDir}
	// A stale capability file from the crashed owner is retired on release.
	require.NoError(t, os.WriteFile(capabilityDir+"/exec-stale", []byte("stale"), 0o600))

	require.NoError(t, git_model.AddDeletedBranch(ctx, 1, "branch2", 2))
	claimed := claimOrdinaryOwner(t, ctx, "ord:branch-delete/1/branch2/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		RepositoryID: 1,
		Ref:          "refs/heads/branch2",
		OldOID:       "985f0301dba5e7b34be866819cd15ad3d8f508ee",
		NewOID:       "0000000000000000000000000000000000000000",
		Family:       FamilyBranchDelete,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, "deleted", assessment.Effect)
	entries, err := os.ReadDir(capabilityDir)
	require.NoError(t, err)
	require.Empty(t, entries)

	idle, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, idle.Owner)
}

func TestRecoverBranchDeleteNoEffect(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	svc := admissionService(t, map[string]string{"refs/heads/branch2": "985f0301dba5e7b34be866819cd15ad3d8f508ee"}, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:branch-delete/1/branch2/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		RepositoryID: 1,
		Ref:          "refs/heads/branch2",
		OldOID:       "985f0301dba5e7b34be866819cd15ad3d8f508ee",
		NewOID:       "0000000000000000000000000000000000000000",
		Family:       FamilyBranchDelete,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryReleased, assessment.Verdict)
	require.Equal(t, model.EffectNotCommitted, assessment.Effect)
}

func TestRecoverBranchDeleteMixedStateFences(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	// The ref still exists at the deleted tip while the database already
	// marks it deleted: an unaccounted ordinary effect.
	svc := admissionService(t, map[string]string{"refs/heads/branch2": "985f0301dba5e7b34be866819cd15ad3d8f508ee"}, 0)

	require.NoError(t, git_model.AddDeletedBranch(ctx, 1, "branch2", 2))
	claimed := claimOrdinaryOwner(t, ctx, "ord:branch-delete/1/branch2/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		RepositoryID: 1,
		Ref:          "refs/heads/branch2",
		OldOID:       "985f0301dba5e7b34be866819cd15ad3d8f508ee",
		NewOID:       "0000000000000000000000000000000000000000",
		Family:       FamilyBranchDelete,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnaccounted, assessment.Reason)

	held, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, claimed.Owner, held.Owner)
}

func TestRecoverBranchDeleteMovedTipFences(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	useIsolatedAppData(t)
	// The live tip moved away from the scope's deleted tip without any
	// participating writer: unaccounted.
	svc := admissionService(t, map[string]string{"refs/heads/branch2": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, 0)

	claimed := claimOrdinaryOwner(t, ctx, "ord:branch-delete/1/branch2/nonce", Scope{
		Kind:         model.OwnerOrdinary,
		RepositoryID: 1,
		Ref:          "refs/heads/branch2",
		OldOID:       "985f0301dba5e7b34be866819cd15ad3d8f508ee",
		NewOID:       "0000000000000000000000000000000000000000",
		Family:       FamilyBranchDelete,
	})
	inhibitDomain(t)

	assessment, err := svc.Recover(ctx, claimed.Owner, claimed.Generation)
	require.NoError(t, err)
	require.Equal(t, RecoveryFenced, assessment.Verdict)
	require.Equal(t, ReasonRecoveryUnaccounted, assessment.Reason)
}
