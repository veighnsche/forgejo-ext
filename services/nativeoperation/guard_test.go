// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"errors"
	"testing"

	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	execcontext "forgejo.org/modules/nativeoperation"

	"github.com/stretchr/testify/require"
)

func TestOrdinaryOwnershipSerializesAndReleases(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := admissionService(t, nil, 0)

	start, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, start.Idle)

	ran := false
	err = svc.WithOrdinaryOwnership(ctx, "branch", "1/topic", Scope{RepositoryID: 1, Ref: "refs/heads/topic"}, func(ctx context.Context) error {
		ran = true
		exec := execcontext.FromContext(ctx)
		require.NotNil(t, exec)
		require.NotEmpty(t, exec.CapabilityPath)
		held, err := svc.ReadNativeRevision(ctx)
		require.NoError(t, err)
		require.False(t, held.Idle)
		require.Equal(t, start.Revision+1, held.Revision)
		// Nested participation reuses the enclosing ownership.
		return svc.WithOrdinaryOwnership(ctx, "branch", "1/nested", Scope{}, func(context.Context) error {
			return nil
		})
	})
	require.NoError(t, err)
	require.True(t, ran)

	idle, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, idle.Idle)
	require.Equal(t, start.Revision+1, idle.Revision)
}

func TestOrdinaryOwnershipRefusesWhenBusy(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := admissionService(t, nil, 0)

	_, err := model.ClaimOrdinary(ctx, "ord:test/holder", `{}`, "v")
	require.NoError(t, err)
	ran := false
	err = svc.WithOrdinaryOwnership(ctx, "branch", "1/topic", Scope{RepositoryID: 1, Ref: "refs/heads/topic"}, func(context.Context) error {
		ran = true
		return nil
	})
	require.Error(t, err)
	require.True(t, IsBusy(err))
	require.False(t, ran)

	// A failing writer still releases its owner.
	require.NoError(t, model.ReleaseOwner(ctx, "ord:test/holder"))
	boom := errors.New("writer failed")
	err = svc.WithOrdinaryOwnership(ctx, "branch", "1/topic", Scope{RepositoryID: 1, Ref: "refs/heads/topic"}, func(context.Context) error {
		return boom
	})
	require.ErrorIs(t, err, boom)
	idle, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, idle.Idle)
}

func TestOrdinaryOwnershipRefusesWhileInhibited(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := admissionService(t, nil, 0)
	useIsolatedAppData(t)

	start, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, start.Idle)
	inhibitDomain(t)

	ran := false
	err = svc.WithOrdinaryOwnership(ctx, "branch", "1/topic", Scope{RepositoryID: 1, Ref: "refs/heads/topic"}, func(context.Context) error {
		ran = true
		return nil
	})
	require.Error(t, err)
	require.True(t, IsBusy(err))
	require.ErrorIs(t, err, model.ErrInhibited)
	require.False(t, ran)

	idle, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, idle.Idle)
	require.Equal(t, start.Revision, idle.Revision)
}
