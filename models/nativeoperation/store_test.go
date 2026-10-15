// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation_test

import (
	"os"
	"path/filepath"
	"testing"

	nativeoperation_model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	execcontext "forgejo.org/modules/nativeoperation"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/require"
)

const testInstallation = "11111111-2222-4333-8444-555555555555"

func testOperation(id string, revision int64) *nativeoperation_model.Operation {
	return &nativeoperation_model.Operation{
		InstallationID:         testInstallation,
		OperationID:            id,
		Kind:                   nativeoperation_model.KindMerge,
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

	start, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, start.Owner)

	claimed, err := nativeoperation_model.ClaimConditional(ctx, testOperation("op-claim", start.Revision), "cond:"+testInstallation+"/op-claim", `{"kind":"conditional"}`, "verifier")
	require.NoError(t, err)
	require.Equal(t, start.Revision+1, claimed.Revision)
	require.Equal(t, "cond:"+testInstallation+"/op-claim", claimed.Owner)

	busy, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, claimed.Revision, busy.Revision)
	require.NotEmpty(t, busy.Owner)

	// A competing claim refuses before any effect and records nothing.
	_, err = nativeoperation_model.ClaimConditional(ctx, testOperation("op-other", busy.Revision), "cond:"+testInstallation+"/op-other", `{}`, "v")
	require.ErrorIs(t, err, nativeoperation_model.ErrBusy)
	missing, err := nativeoperation_model.LookupOperation(ctx, testInstallation, "op-other")
	require.NoError(t, err)
	require.Nil(t, missing)

	// Only the exact owner releases.
	require.ErrorIs(t, nativeoperation_model.ReleaseOwner(ctx, "cond:"+testInstallation+"/op-other"), nativeoperation_model.ErrWrongOwner)
	require.NoError(t, nativeoperation_model.ReleaseOwner(ctx, "cond:"+testInstallation+"/op-claim"))
	idle, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, idle.Owner)
	require.Equal(t, claimed.Revision, idle.Revision)
}

func TestClaimRefusesStaleRevisionWithoutRecording(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	start, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	_, err = nativeoperation_model.ClaimConditional(ctx, testOperation("op-stale", start.Revision-1), "cond:"+testInstallation+"/op-stale", `{}`, "v")
	require.ErrorIs(t, err, nativeoperation_model.ErrStaleRevision)

	idle, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, idle.Owner)
	require.Equal(t, start.Revision, idle.Revision)
}

func TestTombstoneAndDuplicateInsert(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	tombstone, err := nativeoperation_model.InsertTombstone(ctx, testInstallation, "op-cancel-first")
	require.NoError(t, err)
	require.False(t, tombstone.Submitted)
	require.True(t, tombstone.Revoked)
	require.Equal(t, nativeoperation_model.EffectNotCommitted, tombstone.EffectState)
	require.Equal(t, nativeoperation_model.ReasonCancelledBeforeSubmit, tombstone.Reason)

	again, err := nativeoperation_model.InsertTombstone(ctx, testInstallation, "op-cancel-first")
	require.ErrorIs(t, err, nativeoperation_model.ErrDuplicateOperation)
	require.Equal(t, tombstone.ID, again.ID)
}

func TestAdmissionOrdersAgainstCancellation(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	start, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	owner := "cond:" + testInstallation + "/op-admit"
	_, err = nativeoperation_model.ClaimConditional(ctx, testOperation("op-admit", start.Revision), owner, `{}`, "v")
	require.NoError(t, err)

	// Cancellation winning first prevents admission but commits the
	// ordering truth.
	revoked, err := nativeoperation_model.RevokeOperation(ctx, testInstallation, "op-admit")
	require.NoError(t, err)
	require.True(t, revoked.Revoked)
	lost, admitted, err := nativeoperation_model.RecordAdmissionAttempt(ctx, testInstallation, "op-admit", owner, true, "")
	require.NoError(t, err)
	require.False(t, admitted)
	require.Equal(t, nativeoperation_model.ReasonCancelledBeforeAdmission, lost.Reason)
	recorded, err := nativeoperation_model.LookupOperation(ctx, testInstallation, "op-admit")
	require.NoError(t, err)
	require.False(t, recorded.Admitted)
	require.Equal(t, nativeoperation_model.ReasonCancelledBeforeAdmission, recorded.Reason)

	// A second admission of the same operation is also lost.
	owner2 := "cond:" + testInstallation + "/op-admit-2"
	current, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	require.NoError(t, nativeoperation_model.ReleaseOwner(ctx, owner))
	_, err = nativeoperation_model.ClaimConditional(ctx, testOperation("op-admit-2", current.Revision), owner2, `{}`, "v")
	require.NoError(t, err)
	admittedOp, admitted, err := nativeoperation_model.RecordAdmissionAttempt(ctx, testInstallation, "op-admit-2", owner2, true, "")
	require.NoError(t, err)
	require.True(t, admitted)
	require.True(t, admittedOp.Admitted)
	duplicate, admitted, err := nativeoperation_model.RecordAdmissionAttempt(ctx, testInstallation, "op-admit-2", owner2, true, "")
	require.NoError(t, err)
	require.False(t, admitted)
	require.Equal(t, nativeoperation_model.ReasonDuplicateAdmission, duplicate.Reason)

	// Terminal recording releases the exact owner in the same transaction.
	terminal, err := nativeoperation_model.SetTerminal(ctx, testInstallation, "op-admit-2", nativeoperation_model.TerminalOutcome{
		EffectState:  nativeoperation_model.EffectCommitted,
		Cancellation: nativeoperation_model.CancellationNone,
		Completion:   nativeoperation_model.CompletionComplete,
		Receipt:      `{}`,
	}, owner2)
	require.NoError(t, err)
	require.True(t, terminal.IsTerminal())
	idle, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, idle.Owner)

	// Terminal rows are stable: a second terminal write keeps the first.
	stable, err := nativeoperation_model.SetTerminal(ctx, testInstallation, "op-admit-2", nativeoperation_model.TerminalOutcome{
		EffectState: nativeoperation_model.EffectNotCommitted,
	}, "")
	require.NoError(t, err)
	require.Equal(t, nativeoperation_model.EffectCommitted, stable.EffectState)
}

func TestOrdinaryClaimAndRelease(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	start, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	claimed, err := nativeoperation_model.ClaimOrdinary(ctx, "ord:branch/1/main/x", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	require.Equal(t, start.Revision+1, claimed.Revision)
	require.Equal(t, nativeoperation_model.OwnerOrdinary, claimed.OwnerKind)

	_, err = nativeoperation_model.ClaimOrdinary(ctx, "ord:branch/1/other/y", `{}`, "v")
	require.ErrorIs(t, err, nativeoperation_model.ErrBusy)
	require.NoError(t, nativeoperation_model.ReleaseOwner(ctx, "ord:branch/1/main/x"))
}

func useIsolatedAppData(t *testing.T) {
	t.Helper()
	previous := setting.AppDataPath
	setting.AppDataPath = t.TempDir()
	t.Cleanup(func() { setting.AppDataPath = previous })
}

func TestClaimsRefuseWhileInhibited(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()

	start, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(setting.AppDataPath, "nativeop-offline"), []byte("recovery\n"), 0o600))
	require.True(t, nativeoperation_model.OfflineInhibited())

	_, err = nativeoperation_model.ClaimConditional(ctx, testOperation("op-inhibited", start.Revision), "cond:"+testInstallation+"/op-inhibited", `{}`, "v")
	require.ErrorIs(t, err, nativeoperation_model.ErrInhibited)
	_, err = nativeoperation_model.ClaimOrdinary(ctx, "ord:branch/1/x", `{}`, "v")
	require.ErrorIs(t, err, nativeoperation_model.ErrInhibited)

	// Inhibited claims record nothing and move no revision.
	missing, err := nativeoperation_model.LookupOperation(ctx, testInstallation, "op-inhibited")
	require.NoError(t, err)
	require.Nil(t, missing)
	idle, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, idle.Owner)
	require.Equal(t, start.Revision, idle.Revision)

	// Lifting inhibition reopens claims.
	require.NoError(t, os.Remove(nativeoperation_model.OfflineMarkerPath()))
	require.False(t, nativeoperation_model.OfflineInhibited())
	_, err = nativeoperation_model.ClaimOrdinary(ctx, "ord:branch/1/x", `{}`, "v")
	require.NoError(t, err)
}

func TestRequireHeldOwnership(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	require.NoError(t, nativeoperation_model.RequireHeldOwnership(ctx))

	claimed, err := nativeoperation_model.ClaimOrdinary(ctx, "ord:branch/1/held", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)

	require.ErrorIs(t, nativeoperation_model.RequireHeldOwnership(ctx), nativeoperation_model.ErrBusy)
	nested := execcontext.NewContext(ctx, &execcontext.Execution{Owner: claimed.Owner, Generation: claimed.Generation})
	require.NoError(t, nativeoperation_model.RequireHeldOwnership(nested))
	stale := execcontext.NewContext(ctx, &execcontext.Execution{Owner: claimed.Owner, Generation: claimed.Generation + 1})
	require.ErrorIs(t, nativeoperation_model.RequireHeldOwnership(stale), nativeoperation_model.ErrBusy)
	foreign := execcontext.NewContext(ctx, &execcontext.Execution{Owner: "ord:branch/1/other", Generation: claimed.Generation})
	require.ErrorIs(t, nativeoperation_model.RequireHeldOwnership(foreign), nativeoperation_model.ErrBusy)

	require.NoError(t, nativeoperation_model.ReleaseOwner(ctx, claimed.Owner))
	require.NoError(t, nativeoperation_model.RequireHeldOwnership(ctx))
}

func TestReleaseExactOwner(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	claimed, err := nativeoperation_model.ClaimOrdinary(ctx, "ord:branch/1/exact", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)

	require.ErrorIs(t, nativeoperation_model.ReleaseExactOwner(ctx, "", claimed.Generation), nativeoperation_model.ErrWrongOwner)
	require.ErrorIs(t, nativeoperation_model.ReleaseExactOwner(ctx, claimed.Owner, 0), nativeoperation_model.ErrWrongOwner)
	require.ErrorIs(t, nativeoperation_model.ReleaseExactOwner(ctx, "ord:branch/1/other", claimed.Generation), nativeoperation_model.ErrWrongOwner)
	require.ErrorIs(t, nativeoperation_model.ReleaseExactOwner(ctx, claimed.Owner, claimed.Generation+1), nativeoperation_model.ErrWrongOwner)

	held, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Equal(t, claimed.Owner, held.Owner)

	require.NoError(t, nativeoperation_model.ReleaseExactOwner(ctx, claimed.Owner, claimed.Generation))
	idle, err := nativeoperation_model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, idle.Owner)
	require.Equal(t, claimed.Revision, idle.Revision)
}
