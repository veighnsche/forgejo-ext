// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation_test

import (
	"context"
	"errors"
	"testing"

	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func claimTestOperation(t *testing.T, id string) (owner string, generation int64) {
	t.Helper()
	ctx := t.Context()
	start, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, start.Owner)
	owner = "cond:" + testInstallation + "/" + id
	claimed, err := model.ClaimConditional(ctx, testOperation(id, start.Revision), owner, `{"kind":"conditional"}`, "verifier")
	require.NoError(t, err)
	return owner, claimed.Generation
}

func TestCommitPRCreatePrimaryCommitsReceiptAndRetainsOwner(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	owner, generation := claimTestOperation(t, "op-prcreate-commit")

	inserted := false
	recorded, err := model.CommitPRCreatePrimary(ctx, testInstallation, "op-prcreate-commit", owner, 1000000000, func(ctx context.Context) (string, error) {
		inserted = true
		return `{"pr_id":7}`, nil
	})
	require.NoError(t, err)
	require.True(t, inserted)
	require.True(t, recorded.Admitted)
	require.Equal(t, model.EffectCommitted, recorded.EffectState)
	require.Equal(t, model.CompletionPending, recorded.Completion)
	require.Equal(t, `{"pr_id":7}`, recorded.Receipt)

	// The owner stays held for bounded completion.
	reservation, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, owner, reservation.Owner)
	require.Equal(t, generation, reservation.Generation)
}

func TestCommitPRCreatePrimaryRefusesRevokedWithoutInsert(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	owner, _ := claimTestOperation(t, "op-prcreate-revoked")

	_, err := model.RevokeOperation(ctx, testInstallation, "op-prcreate-revoked")
	require.NoError(t, err)

	inserted := false
	_, err = model.CommitPRCreatePrimary(ctx, testInstallation, "op-prcreate-revoked", owner, 1000000000, func(ctx context.Context) (string, error) {
		inserted = true
		return `{}`, nil
	})
	require.ErrorIs(t, err, model.ErrAdmissionLost)
	require.False(t, inserted)

	op, err := model.LookupOperation(ctx, testInstallation, "op-prcreate-revoked")
	require.NoError(t, err)
	require.Equal(t, model.EffectPending, op.EffectState)
}

func TestCommitPRCreatePrimaryRefusesExpiredWithoutInsert(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	owner, _ := claimTestOperation(t, "op-prcreate-expired")

	inserted := false
	_, err := model.CommitPRCreatePrimary(ctx, testInstallation, "op-prcreate-expired", owner, 2000000001, func(ctx context.Context) (string, error) {
		inserted = true
		return `{}`, nil
	})
	require.ErrorIs(t, err, model.ErrOperationExpired)
	require.False(t, inserted)
}

func TestCommitPRCreatePrimaryRollsBackInsertFailure(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	owner, _ := claimTestOperation(t, "op-prcreate-rollback")

	boom := errors.New("insert failed")
	_, err := model.CommitPRCreatePrimary(ctx, testInstallation, "op-prcreate-rollback", owner, 1000000000, func(ctx context.Context) (string, error) {
		return "", boom
	})
	require.ErrorIs(t, err, boom)

	op, err := model.LookupOperation(ctx, testInstallation, "op-prcreate-rollback")
	require.NoError(t, err)
	require.False(t, op.Admitted)
	require.Equal(t, model.EffectPending, op.EffectState)
	require.Empty(t, op.Receipt)
}

func TestSetCompletionAndReleaseFinalizesExactOwner(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	owner, generation := claimTestOperation(t, "op-prcreate-finalize")

	_, err := model.CommitPRCreatePrimary(ctx, testInstallation, "op-prcreate-finalize", owner, 1000000000, func(ctx context.Context) (string, error) {
		return `{"pr_id":9}`, nil
	})
	require.NoError(t, err)

	recorded, err := model.SetCompletionAndRelease(ctx, testInstallation, "op-prcreate-finalize", owner, generation, model.CompletionComplete)
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, recorded.EffectState)
	require.Equal(t, model.CompletionComplete, recorded.Completion)

	reservation, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, reservation.Owner)
}

func TestSetCompletionAndReleaseRefusesWrongGeneration(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	owner, generation := claimTestOperation(t, "op-prcreate-generation")

	_, err := model.CommitPRCreatePrimary(ctx, testInstallation, "op-prcreate-generation", owner, 1000000000, func(ctx context.Context) (string, error) {
		return `{}`, nil
	})
	require.NoError(t, err)

	_, err = model.SetCompletionAndRelease(ctx, testInstallation, "op-prcreate-generation", owner, generation+1, model.CompletionComplete)
	require.ErrorIs(t, err, model.ErrWrongOwner)

	// The failed finalize leaves the operation unfinished and held.
	op, err := model.LookupOperation(ctx, testInstallation, "op-prcreate-generation")
	require.NoError(t, err)
	require.Equal(t, model.CompletionPending, op.Completion)
	reservation, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, owner, reservation.Owner)
}
