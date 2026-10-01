// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"testing"

	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	execcontext "forgejo.org/modules/nativeoperation"

	"github.com/stretchr/testify/require"
)

// TestCollabOwnershipPrimitive proves the pull-local collaboration
// mirror honors the claim protocol: it runs the writer when idle,
// refuses before any effect while a foreign owner holds the
// reservation, and reuses an enclosing execution without claiming.
func TestCollabOwnershipPrimitive(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	ran := false
	require.NoError(t, withCollabOwnership(ctx, CollabPullResource(3, "refresh"), 1, func(ctx context.Context) error {
		ran = true
		require.NotNil(t, execcontext.FromContext(ctx))
		return nil
	}))
	require.True(t, ran)

	claimed, err := model.ClaimOrdinary(ctx, "ord:branch-delete/1/x", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	defer func() { _ = model.ReleaseOwner(ctx, claimed.Owner) }()

	ran = false
	require.ErrorIs(t, withCollabOwnership(ctx, CollabPullResource(3, "refresh"), 1, func(ctx context.Context) error {
		ran = true
		return nil
	}), model.ErrBusy)
	require.False(t, ran)

	nested := execcontext.NewContext(ctx, &execcontext.Execution{Owner: claimed.Owner})
	require.NoError(t, withCollabOwnership(nested, CollabPullResource(3, "refresh"), 1, func(ctx context.Context) error {
		return nil
	}))
}

// TestCollabOwnershipCanceledContext proves a canceled caller never
// strands the reservation: a pre-canceled claim refuses before any
// effect, and a cancellation racing the writer still releases.
func TestCollabOwnershipCanceledContext(t *testing.T) {
	unittest.PrepareTestEnv(t)

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	ran := false
	require.ErrorIs(t, withCollabOwnership(canceled, CollabPullResource(3, "refresh"), 1, func(ctx context.Context) error {
		ran = true
		return nil
	}), context.Canceled)
	require.False(t, ran)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, withCollabOwnership(ctx, CollabPullResource(3, "refresh"), 1, func(ctx context.Context) error {
		cancel()
		return nil
	}))

	idle, err := model.ReadReservation(context.WithoutCancel(ctx))
	require.NoError(t, err)
	require.Empty(t, idle.Owner)
}
