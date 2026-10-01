// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation_test

import (
	"testing"

	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

const testInstallation = "11111111-2222-4333-8444-555555555555"

func testOperation(id string, revision int64) *model.Operation {
	return &model.Operation{
		InstallationID:         testInstallation,
		OperationID:            id,
		Kind:                   model.KindMerge,
		ActorID:                2,
		RepositoryID:           1,
		TokenID:                9,
		CredentialFingerprint:  "fingerprint",
		AuthRevision:           "rev-1",
		ExpectedNativeRevision: revision,
		NotAfter:               2000000000,
		IntentDigest:           "digest-" + id,
		Intent:                 `{"operation_id":"` + id + `"}`,
	}
}

func TestReservationClaimAdvancesRevision(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	start, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, start.Owner)

	claimed, err := model.ClaimConditional(ctx, testOperation("op-claim", start.Revision), "cond:"+testInstallation+"/op-claim", `{"kind":"conditional"}`, "verifier")
	require.NoError(t, err)
	require.Equal(t, start.Revision+1, claimed.Revision)
	require.Equal(t, "cond:"+testInstallation+"/op-claim", claimed.Owner)

	busy, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, claimed.Revision, busy.Revision)
	require.NotEmpty(t, busy.Owner)

	// A competing claim refuses before any effect and records nothing.
	_, err = model.ClaimConditional(ctx, testOperation("op-other", busy.Revision), "cond:"+testInstallation+"/op-other", `{}`, "v")
	require.ErrorIs(t, err, model.ErrBusy)
	missing, err := model.LookupOperation(ctx, testInstallation, "op-other")
	require.NoError(t, err)
	require.Nil(t, missing)

	// Only the exact owner releases.
	require.ErrorIs(t, model.ReleaseOwner(ctx, "cond:"+testInstallation+"/op-other"), model.ErrWrongOwner)
	require.NoError(t, model.ReleaseOwner(ctx, "cond:"+testInstallation+"/op-claim"))
	idle, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, idle.Owner)
	require.Equal(t, claimed.Revision, idle.Revision)
}

func TestClaimRefusesStaleRevisionWithoutRecording(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	start, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	_, err = model.ClaimConditional(ctx, testOperation("op-stale", start.Revision-1), "cond:"+testInstallation+"/op-stale", `{}`, "v")
	require.ErrorIs(t, err, model.ErrStaleRevision)

	idle, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, idle.Owner)
	require.Equal(t, start.Revision, idle.Revision)
}

func TestTombstoneAndDuplicateInsert(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	tombstone, err := model.InsertTombstone(ctx, testInstallation, "op-cancel-first")
	require.NoError(t, err)
	require.False(t, tombstone.Submitted)
	require.True(t, tombstone.Revoked)
	require.Equal(t, model.EffectNotCommitted, tombstone.EffectState)
	require.Equal(t, model.ReasonCancelledBeforeSubmit, tombstone.Reason)

	again, err := model.InsertTombstone(ctx, testInstallation, "op-cancel-first")
	require.ErrorIs(t, err, model.ErrDuplicateOperation)
	require.Equal(t, tombstone.ID, again.ID)
}

func TestAdmissionOrdersAgainstCancellation(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	start, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	owner := "cond:" + testInstallation + "/op-admit"
	_, err = model.ClaimConditional(ctx, testOperation("op-admit", start.Revision), owner, `{}`, "v")
	require.NoError(t, err)

	// Cancellation winning first prevents admission but commits the
	// ordering truth.
	revoked, err := model.RevokeOperation(ctx, testInstallation, "op-admit")
	require.NoError(t, err)
	require.True(t, revoked.Revoked)
	lost, admitted, err := model.RecordAdmissionAttempt(ctx, testInstallation, "op-admit", owner, true, "")
	require.NoError(t, err)
	require.False(t, admitted)
	require.Equal(t, model.ReasonCancelledBeforeAdmission, lost.Reason)
	recorded, err := model.LookupOperation(ctx, testInstallation, "op-admit")
	require.NoError(t, err)
	require.False(t, recorded.Admitted)
	require.Equal(t, model.ReasonCancelledBeforeAdmission, recorded.Reason)

	// A second admission of the same operation is also lost.
	owner2 := "cond:" + testInstallation + "/op-admit-2"
	current, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.NoError(t, model.ReleaseOwner(ctx, owner))
	_, err = model.ClaimConditional(ctx, testOperation("op-admit-2", current.Revision), owner2, `{}`, "v")
	require.NoError(t, err)
	admittedOp, admitted, err := model.RecordAdmissionAttempt(ctx, testInstallation, "op-admit-2", owner2, true, "")
	require.NoError(t, err)
	require.True(t, admitted)
	require.True(t, admittedOp.Admitted)
	duplicate, admitted, err := model.RecordAdmissionAttempt(ctx, testInstallation, "op-admit-2", owner2, true, "")
	require.NoError(t, err)
	require.False(t, admitted)
	require.Equal(t, model.ReasonDuplicateAdmission, duplicate.Reason)

	// Terminal recording releases the exact owner in the same transaction.
	terminal, err := model.SetTerminal(ctx, testInstallation, "op-admit-2", model.TerminalOutcome{
		EffectState:  model.EffectCommitted,
		Cancellation: model.CancellationNone,
		Completion:   model.CompletionComplete,
		Receipt:      `{}`,
	}, owner2)
	require.NoError(t, err)
	require.True(t, terminal.IsTerminal())
	idle, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, idle.Owner)

	// Terminal rows are stable: a second terminal write keeps the first.
	stable, err := model.SetTerminal(ctx, testInstallation, "op-admit-2", model.TerminalOutcome{
		EffectState: model.EffectNotCommitted,
	}, "")
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, stable.EffectState)
}

func TestOrdinaryClaimAndRelease(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	start, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	claimed, err := model.ClaimOrdinary(ctx, "ord:branch/1/main/x", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	require.Equal(t, start.Revision+1, claimed.Revision)
	require.Equal(t, model.OwnerOrdinary, claimed.OwnerKind)

	_, err = model.ClaimOrdinary(ctx, "ord:branch/1/other/y", `{}`, "v")
	require.ErrorIs(t, err, model.ErrBusy)
	require.NoError(t, model.ReleaseOwner(ctx, "ord:branch/1/main/x"))
}
