// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package actions

import (
	"testing"

	nativeoperation_model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func TestTrustScheduleWritersFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	claimed, err := nativeoperation_model.ClaimOrdinary(ctx, "ord:actions-run/test/abc123", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	defer func() { _ = nativeoperation_model.ReleaseOwner(ctx, claimed.Owner) }()

	require.ErrorIs(t, InsertActionUser(ctx, &ActionUser{UserID: 2, RepoID: 1}), nativeoperation_model.ErrBusy)
	require.ErrorIs(t, DeleteActionUserByUserIDAndRepoID(ctx, 2, 1), nativeoperation_model.ErrBusy)
	require.ErrorIs(t, RevokeInactiveActionUser(ctx), nativeoperation_model.ErrBusy)
	require.ErrorIs(t, CreateScheduleTask(ctx, []*ActionSchedule{{RepoID: 1}}), nativeoperation_model.ErrBusy)
	require.ErrorIs(t, DeleteScheduleTaskByRepo(ctx, 1), nativeoperation_model.ErrBusy)
	require.ErrorIs(t, UpdateScheduleSpec(ctx, &ActionScheduleSpec{ID: 1}, "prev", "next"), nativeoperation_model.ErrBusy)
	_, err = FixRunnersWithoutBelongingOwner(ctx)
	require.ErrorIs(t, err, nativeoperation_model.ErrBusy)
	_, err = FixRunnersWithoutBelongingRepo(ctx)
	require.ErrorIs(t, err, nativeoperation_model.ErrBusy)
}

func TestTrustAccessTelemetryProceedsWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	claimed, err := nativeoperation_model.ClaimOrdinary(ctx, "ord:actions-run/test/abc123", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	defer func() { _ = nativeoperation_model.ReleaseOwner(ctx, claimed.Owner) }()

	// The last-access timestamp rides read paths and stays outside the
	// reservation like token last-used timestamps.
	require.NoError(t, MaybeUpdateAccess(ctx, &ActionUser{ID: 1, UserID: 2, RepoID: 1}))
}
