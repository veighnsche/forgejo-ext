// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asymkey

import (
	"testing"

	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func claimTestOwner(t *testing.T) string {
	t.Helper()
	claimed, err := nativeoperation.ClaimOrdinary(t.Context(), "ord:authority/test/abc123", `{"kind":"ordinary","family":"authority"}`, "v")
	require.NoError(t, err)
	t.Cleanup(func() { _ = nativeoperation.ReleaseOwner(t.Context(), claimed.Owner) })
	return claimed.Owner
}

func TestAddPublicKeyFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	_, err := AddPublicKey(ctx, 2, "fence-test-key", "ssh-rsa INVALID", 0)
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
}

func TestDeployKeyWritesFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	_, err := AddDeployKey(ctx, 1, "fence-test-deploy-key", "ssh-rsa INVALID", true)
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
	require.ErrorIs(t, UpdateDeployKeyCols(ctx, &DeployKey{ID: 999}, "fingerprint"), nativeoperation.ErrBusy)
}

func TestDeployKeyTelemetryProceedsWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.NoError(t, UpdateDeployKeyCols(ctx, &DeployKey{ID: 999}, "updated_unix"))
}

func TestGPGKeyWritesFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	_, err := AddGPGKey(ctx, 2, "invalid", "", "")
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
}
