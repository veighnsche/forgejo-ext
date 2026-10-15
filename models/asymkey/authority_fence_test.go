// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asymkey

import (
	"testing"

	nativeoperation_model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func claimTestOwner(t *testing.T) string {
	t.Helper()
	claimed, err := nativeoperation_model.ClaimOrdinary(t.Context(), "ord:authority/test/abc123", `{"kind":"ordinary","family":"authority"}`, "v")
	require.NoError(t, err)
	t.Cleanup(func() { _ = nativeoperation_model.ReleaseOwner(t.Context(), claimed.Owner) })
	return claimed.Owner
}

func TestAddPublicKeyFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	_, err := AddPublicKey(ctx, 2, "fence-test-key", "ssh-rsa INVALID", 0)
	require.ErrorIs(t, err, nativeoperation_model.ErrBusy)
}

func TestDeployKeyWritesFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	_, err := AddDeployKey(ctx, 1, "fence-test-deploy-key", "ssh-rsa INVALID", true)
	require.ErrorIs(t, err, nativeoperation_model.ErrBusy)
	require.ErrorIs(t, UpdateDeployKeyCols(ctx, &DeployKey{ID: 999}, "fingerprint"), nativeoperation_model.ErrBusy)
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
	require.ErrorIs(t, err, nativeoperation_model.ErrBusy)
}
