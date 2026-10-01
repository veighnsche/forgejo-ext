// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package actions

import (
	"testing"

	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func TestRunWritersFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	unittest.AssertExistsAndLoadBean(t, &ActionRun{ID: 791})

	claimed, err := nativeoperation.ClaimOrdinary(ctx, "ord:branch-delete/1/x", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	defer func() { _ = nativeoperation.ReleaseOwner(ctx, claimed.Owner) }()

	require.ErrorIs(t, UpdateRunWithoutNotification(ctx, &ActionRun{ID: 791, Status: StatusFailure}, "status"), nativeoperation.ErrBusy)
	require.ErrorIs(t, UpdateRunApprovalByID(ctx, 791, DoesNotNeedApproval, 1), nativeoperation.ErrBusy)
	require.ErrorIs(t, InsertRun(ctx, &ActionRun{RepoID: 1, OwnerID: 1, TriggerUserID: 1}, nil), nativeoperation.ErrBusy)
	require.ErrorIs(t, InsertRunJobs(ctx, &ActionRun{ID: 791, RepoID: 4, OwnerID: 1}, nil), nativeoperation.ErrBusy)

	fresh := unittest.AssertExistsAndLoadBean(t, &ActionRun{ID: 791})
	require.Equal(t, StatusSuccess, fresh.Status)
	require.False(t, bool(fresh.NeedApproval))
}

func TestRunWritersProceedWhenIdle(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	require.NoError(t, UpdateRunApprovalByID(ctx, 791, DoesNotNeedApproval, 2))
	approved := unittest.AssertExistsAndLoadBean(t, &ActionRun{ID: 791})
	require.Equal(t, int64(2), approved.ApprovedBy)

	loaded, err := GetRunByID(ctx, 791)
	require.NoError(t, err)
	loaded.Status = StatusFailure
	require.NoError(t, UpdateRunWithoutNotification(ctx, loaded, "status"))
	updated := unittest.AssertExistsAndLoadBean(t, &ActionRun{ID: 791})
	require.Equal(t, StatusFailure, updated.Status)
}

func TestGetRunsByScheduleID(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	require.NoError(t, InsertRun(ctx, &ActionRun{
		Title:         "scheduled",
		RepoID:        1,
		OwnerID:       1,
		TriggerUserID: 1,
		ScheduleID:    424242,
		Status:        StatusWaiting,
	}, nil))

	runs, err := GetRunsByScheduleID(ctx, 424242)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, "scheduled", runs[0].Title)

	empty, err := GetRunsByScheduleID(ctx, 424243)
	require.NoError(t, err)
	require.Empty(t, empty)
}
