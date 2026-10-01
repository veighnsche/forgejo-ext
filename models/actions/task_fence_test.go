// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package actions

import (
	"testing"

	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func TestUpdateTaskFencesStateWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	task := unittest.AssertExistsAndLoadBean(t, &ActionTask{ID: 47})
	require.False(t, task.Status.IsDone())

	claimed, err := nativeoperation.ClaimOrdinary(ctx, "ord:branch-delete/1/x", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)

	task.Status = StatusSuccess
	task.Stopped = 1683636700
	require.ErrorIs(t, UpdateTask(ctx, task, "status", "stopped"), nativeoperation.ErrBusy)

	fresh := unittest.AssertExistsAndLoadBean(t, &ActionTask{ID: 47})
	require.NotEqual(t, StatusSuccess, fresh.Status)

	require.NoError(t, nativeoperation.ReleaseOwner(ctx, claimed.Owner))
	require.NoError(t, UpdateTask(ctx, task, "status", "stopped"))
	done := unittest.AssertExistsAndLoadBean(t, &ActionTask{ID: 47})
	require.Equal(t, StatusSuccess, done.Status)
}

func TestUpdateTaskTelemetryProceedsWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	claimed, err := nativeoperation.ClaimOrdinary(ctx, "ord:branch-delete/1/x", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	defer func() { _ = nativeoperation.ReleaseOwner(ctx, claimed.Owner) }()

	task := unittest.AssertExistsAndLoadBean(t, &ActionTask{ID: 47})
	require.NoError(t, UpdateTask(ctx, &ActionTask{ID: task.ID, Updated: 1683636999}, "updated"))
	require.NoError(t, UpdateTask(ctx, &ActionTask{ID: task.ID, LogLength: 999}, "log_length"))

	// Token material is not telemetry: it refuses while held.
	require.ErrorIs(t, UpdateTask(ctx, &ActionTask{ID: task.ID, TokenHash: "x"}, "token_hash"), nativeoperation.ErrBusy)
}

func TestUpdateRunJobFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	unittest.AssertExistsAndLoadBean(t, &ActionRunJob{ID: 192})

	claimed, err := nativeoperation.ClaimOrdinary(ctx, "ord:branch-delete/1/x", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	defer func() { _ = nativeoperation.ReleaseOwner(ctx, claimed.Owner) }()

	_, err = UpdateRunJobWithoutNotification(ctx, &ActionRunJob{ID: 192, Status: StatusFailure}, nil, "status")
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
}
