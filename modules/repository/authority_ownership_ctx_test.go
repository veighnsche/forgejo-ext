// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repository

import (
	"context"
	"testing"

	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

// TestAuthorityOwnershipCanceledContext proves a canceled caller never
// strands the reservation: a pre-canceled claim refuses before any
// effect, and a cancellation racing the writer still releases.
func TestAuthorityOwnershipCanceledContext(t *testing.T) {
	unittest.PrepareTestEnv(t)

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	ran := false
	require.ErrorIs(t, withAuthorityOwnership(canceled, 1, 4, func(ctx context.Context) error {
		ran = true
		return nil
	}), context.Canceled)
	require.False(t, ran)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, withAuthorityOwnership(ctx, 1, 4, func(ctx context.Context) error {
		cancel()
		return nil
	}))

	idle, err := model.ReadReservation(context.WithoutCancel(ctx))
	require.NoError(t, err)
	require.Empty(t, idle.Owner)
}
