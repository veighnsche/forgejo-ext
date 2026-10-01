// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package auth

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

func TestAccessTokenWritesFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.ErrorIs(t, NewAccessToken(ctx, &AccessToken{UID: 3, Name: "Fence Token"}), nativeoperation.ErrBusy)
	require.ErrorIs(t, DeleteAccessTokenByID(ctx, 2, 2), nativeoperation.ErrBusy)
	unittest.AssertExistsAndLoadBean(t, &AccessToken{ID: 2})
}

func TestAccessTokenTelemetryProceedsWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	token := unittest.AssertExistsAndLoadBean(t, &AccessToken{ID: 2})
	require.NoError(t, token.UpdateLastUsed(ctx))
}

func TestAuthSourceWritesFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.ErrorIs(t, CreateSource(ctx, &Source{Name: "fence-test-source"}), nativeoperation.ErrBusy)
	require.ErrorIs(t, UpdateSource(ctx, &Source{ID: 1, Name: "fence-test-source"}), nativeoperation.ErrBusy)
}

func TestOAuth2ApplicationWritesFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	_, err := CreateOAuth2Application(ctx, CreateOAuth2ApplicationOptions{Name: "fence-test-app", UserID: 2})
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
	require.ErrorIs(t, DeleteOAuth2Application(ctx, 1, 2), nativeoperation.ErrBusy)
}

func TestTwoFactorWritesFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.ErrorIs(t, NewTwoFactor(ctx, &TwoFactor{UID: 44}, "secret"), nativeoperation.ErrBusy)
	require.ErrorIs(t, DeleteTwoFactorByID(ctx, 1, 2), nativeoperation.ErrBusy)
}

func TestWebAuthnCredentialWritesFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	_, err := CreateCredential(ctx, 2, "fence-test-credential", nil)
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
	_, err = DeleteCredential(ctx, 1, 2)
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
}
