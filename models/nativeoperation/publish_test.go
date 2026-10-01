// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation_test

import (
	"testing"
	"time"

	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func waitingTestOperation(id string, revision int64) *model.Operation {
	op := testOperation(id, revision)
	op.Kind = model.KindRefPublish
	return op
}

func TestInsertWaitingOccupiesNothing(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	start, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, start.Owner)

	recorded, err := model.InsertWaitingOperation(ctx, waitingTestOperation("op-wait", start.Revision))
	require.NoError(t, err)
	require.True(t, recorded.Submitted)
	require.Equal(t, model.EffectPending, recorded.EffectState)

	idle, err := model.ReadReservation(ctx)
	require.NoError(t, err)
	require.Empty(t, idle.Owner)
	require.Equal(t, start.Revision, idle.Revision)

	existing, err := model.InsertWaitingOperation(ctx, waitingTestOperation("op-wait", start.Revision))
	require.ErrorIs(t, err, model.ErrDuplicateOperation)
	require.Equal(t, recorded.ID, existing.ID)
}

func TestClaimPublishReceive(t *testing.T) {
	claim := func(t *testing.T, id, owner string, now int64) (*model.Operation, *model.Reservation, error) {
		t.Helper()
		return model.ClaimPublishReceive(t.Context(), testInstallation, id, owner, `{"kind":"conditional"}`, "verifier", now)
	}

	t.Run("SuccessAdvancesRevision", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		start, err := model.ReadReservation(ctx)
		require.NoError(t, err)
		_, err = model.InsertWaitingOperation(ctx, waitingTestOperation("op-claim-ok", start.Revision))
		require.NoError(t, err)
		op, claimed, err := claim(t, "op-claim-ok", "cond:"+testInstallation+"/op-claim-ok", time.Now().Unix())
		require.NoError(t, err)
		require.Equal(t, model.EffectPending, op.EffectState)
		require.Equal(t, start.Revision+1, claimed.Revision)
		require.Equal(t, "cond:"+testInstallation+"/op-claim-ok", claimed.Owner)
	})

	t.Run("BusyRecordsNothing", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		start, err := model.ReadReservation(ctx)
		require.NoError(t, err)
		_, err = model.InsertWaitingOperation(ctx, waitingTestOperation("op-claim-busy", start.Revision))
		require.NoError(t, err)
		_, err = model.ClaimOrdinary(ctx, "ord:other/holder", `{}`, "v")
		require.NoError(t, err)
		_, _, err = claim(t, "op-claim-busy", "cond:"+testInstallation+"/op-claim-busy", time.Now().Unix())
		require.ErrorIs(t, err, model.ErrBusy)
		op, err := model.LookupOperation(ctx, testInstallation, "op-claim-busy")
		require.NoError(t, err)
		require.Equal(t, model.EffectPending, op.EffectState)
	})

	t.Run("StaleRevision", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		start, err := model.ReadReservation(ctx)
		require.NoError(t, err)
		_, err = model.InsertWaitingOperation(ctx, waitingTestOperation("op-claim-stale", start.Revision))
		require.NoError(t, err)
		_, err = model.ClaimOrdinary(ctx, "ord:other/advance", `{}`, "v")
		require.NoError(t, err)
		require.NoError(t, model.ReleaseOwner(ctx, "ord:other/advance"))
		_, _, err = claim(t, "op-claim-stale", "cond:"+testInstallation+"/op-claim-stale", time.Now().Unix())
		require.ErrorIs(t, err, model.ErrStaleRevision)
	})

	t.Run("RevokedAdmittedAndMissingLose", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		now := time.Now().Unix()
		start, err := model.ReadReservation(ctx)
		require.NoError(t, err)

		_, err = model.InsertWaitingOperation(ctx, waitingTestOperation("op-claim-revoked", start.Revision))
		require.NoError(t, err)
		_, err = model.RevokeOperation(ctx, testInstallation, "op-claim-revoked")
		require.NoError(t, err)
		_, _, err = claim(t, "op-claim-revoked", "cond:"+testInstallation+"/op-claim-revoked", now)
		require.ErrorIs(t, err, model.ErrAdmissionLost)

		_, _, err = claim(t, "op-claim-missing", "cond:"+testInstallation+"/op-claim-missing", now)
		require.ErrorIs(t, err, model.ErrAdmissionLost)
	})

	t.Run("Expired", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		start, err := model.ReadReservation(ctx)
		require.NoError(t, err)
		op := waitingTestOperation("op-claim-expired", start.Revision)
		op.NotAfter = time.Now().Unix() - 1
		_, err = model.InsertWaitingOperation(ctx, op)
		require.NoError(t, err)
		_, _, err = claim(t, "op-claim-expired", "cond:"+testInstallation+"/op-claim-expired", time.Now().Unix())
		require.ErrorIs(t, err, model.ErrOperationExpired)
	})
}
