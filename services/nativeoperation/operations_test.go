// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	authmodel "forgejo.org/models/extensionauth"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

const operationTestInstallation = "11111111-2222-4333-8444-555555555555"

func testDecision() authmodel.SubmissionDecision {
	return authmodel.SubmissionDecision{TokenID: 9, ActorID: 2, CredentialFingerprint: "fingerprint"}
}

func testIntent(t *testing.T, id string, revision int64) *ValidIntent {
	t.Helper()
	now := time.Now().Unix()
	intent, err := ValidateIntent(id, 2, 1, model.KindMerge, "rev-1", revision, now+300, mergePayload(t, nil), now)
	require.NoError(t, err)
	return intent
}

func TestGetUnknownIsNotObserved(t *testing.T) {
	unittest.PrepareTestEnv(t)
	svc := NewService()
	lookup, err := svc.Get(t.Context(), operationTestInstallation, "op-missing")
	require.NoError(t, err)
	require.Equal(t, sdk.BackgroundOutcomeNotObserved, lookup.Status)
	require.Nil(t, lookup.Record)
}

func TestCancelBeforeSubmitBlocksDelayedSubmission(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	cancelled, err := svc.Cancel(ctx, operationTestInstallation, "op-early-cancel")
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, cancelled.Outcome)
	require.Equal(t, model.ReasonCancelledBeforeSubmit, cancelled.ReasonCode)
	require.Empty(t, cancelled.ActorID)
	require.Empty(t, cancelled.RepositoryID)
	require.Empty(t, cancelled.Kind)

	observation, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	_, err = svc.Submit(ctx, testDecision(), operationTestInstallation, testIntent(t, "op-early-cancel", observation.Revision))
	require.ErrorIs(t, err, ErrCancelledBeforeSubmit)

	lookup, err := svc.Get(ctx, operationTestInstallation, "op-early-cancel")
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, lookup.Status)
	require.NotNil(t, lookup.Record)
	require.Equal(t, model.CancellationCancelled, lookup.Record.CancellationStatus)
}

func TestSubmitRejectsChangedIntentAndReplaysIdentical(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	observation, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	intent := testIntent(t, "op-replay", observation.Revision)
	op := &model.Operation{
		InstallationID:         operationTestInstallation,
		OperationID:            intent.OperationID,
		Kind:                   intent.Kind,
		ActorID:                2,
		RepositoryID:           1,
		TokenID:                9,
		CredentialFingerprint:  "fingerprint",
		AuthRevision:           intent.AuthRevision,
		ExpectedNativeRevision: intent.ExpectedNativeRevision,
		NotAfter:               intent.NotAfter,
		IntentDigest:           intent.Digest,
		Intent:                 intent.Canonical,
	}
	_, err = model.ClaimConditional(ctx, op, conditionalOwner(operationTestInstallation, intent.OperationID), `{}`, "verifier")
	require.NoError(t, err)

	replay, err := svc.Submit(ctx, testDecision(), operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, model.EffectPending, replay.Outcome)
	require.Equal(t, intent.Digest, replay.IntentDigest)

	changed := testIntent(t, "op-replay", observation.Revision)
	changed.Digest = "different-digest"
	_, err = svc.Submit(ctx, testDecision(), operationTestInstallation, changed)
	require.ErrorIs(t, err, ErrIntentConflict)
}

func TestSubmitRefusesStaleRevisionAndBusy(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	observation, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	// An intervening participating writer advances the revision first.
	advanced, err := model.ClaimOrdinary(ctx, "ord:test/advance", `{}`, "v")
	require.NoError(t, err)
	require.NoError(t, model.ReleaseOwner(ctx, "ord:test/advance"))

	stale, err := svc.Submit(ctx, testDecision(), operationTestInstallation, testIntent(t, "op-stale-submit", observation.Revision))
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, stale.Outcome)
	require.Equal(t, model.ReasonStaleNativeRevision, stale.ReasonCode)
	require.Equal(t, "2", stale.ActorID)

	idle, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, idle.Idle)
	require.Equal(t, advanced.Revision, idle.Revision)

	_, err = model.ClaimOrdinary(ctx, "ord:test/holder", `{}`, "v")
	require.NoError(t, err)
	_, err = svc.Submit(ctx, testDecision(), operationTestInstallation, testIntent(t, "op-busy-submit", idle.Revision+1))
	require.ErrorIs(t, err, ErrBusy)
	missing, err := model.LookupOperation(ctx, operationTestInstallation, "op-busy-submit")
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestSubmitReportsUnimplementedKind(t *testing.T) {
	unittest.PrepareTestEnv(t)
	svc := NewService()
	// Every valid kind is implemented, so the unimplemented-kind probe
	// is a hand-built intent: Submit must still report the missing
	// stage rather than inventing a receipt.
	intent := &ValidIntent{OperationID: "op-future", Kind: "pull_request.future"}
	_, err := svc.Submit(t.Context(), testDecision(), operationTestInstallation, intent)
	require.ErrorIs(t, err, ErrKindUnavailable)
}

func TestCancelPendingStaysPendingAndTerminalMaps(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	observation, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	intent := testIntent(t, "op-cancel-pending", observation.Revision)
	op := &model.Operation{
		InstallationID:         operationTestInstallation,
		OperationID:            intent.OperationID,
		Kind:                   intent.Kind,
		ActorID:                2,
		RepositoryID:           1,
		IntentDigest:           intent.Digest,
		Intent:                 intent.Canonical,
		ExpectedNativeRevision: intent.ExpectedNativeRevision,
		NotAfter:               intent.NotAfter,
		AuthRevision:           intent.AuthRevision,
	}
	owner := conditionalOwner(operationTestInstallation, intent.OperationID)
	_, err = model.ClaimConditional(ctx, op, owner, `{}`, "verifier")
	require.NoError(t, err)

	pending, err := svc.Cancel(ctx, operationTestInstallation, intent.OperationID)
	require.NoError(t, err)
	require.Equal(t, model.EffectPending, pending.Outcome)
	require.Equal(t, model.CancellationPending, pending.CancellationStatus)

	committed, err := model.SetTerminal(ctx, operationTestInstallation, intent.OperationID, model.TerminalOutcome{
		EffectState: model.EffectCommitted,
		Receipt:     `{}`,
	}, owner)
	require.NoError(t, err)
	require.True(t, committed.Revoked)
	late, err := svc.Cancel(ctx, operationTestInstallation, intent.OperationID)
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, late.Outcome)
	require.Equal(t, model.CancellationTooLate, late.CancellationStatus)

	// A terminal refusal without prior cancellation reports cancelled once
	// the owning installation cancels it.
	refusedOp := &model.Operation{
		InstallationID: operationTestInstallation, OperationID: "op-refused",
		Kind: model.KindMerge, ActorID: 2, RepositoryID: 1, Submitted: true,
		EffectState: model.EffectNotCommitted, Reason: model.ReasonStaleHead,
	}
	_, err = model.InsertRefusedOperation(ctx, refusedOp)
	require.NoError(t, err)
	lookup, err := svc.Get(ctx, operationTestInstallation, "op-refused")
	require.NoError(t, err)
	require.Equal(t, model.CancellationNone, lookup.Record.CancellationStatus)
	cancelled, err := svc.Cancel(ctx, operationTestInstallation, "op-refused")
	require.NoError(t, err)
	require.Equal(t, model.CancellationCancelled, cancelled.CancellationStatus)
}
