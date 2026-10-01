// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user_test

import (
	"testing"

	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/require"
)

func claimTestOwner(t *testing.T) string {
	t.Helper()
	claimed, err := nativeoperation.ClaimOrdinary(t.Context(), "ord:authority/test/abc123", `{"kind":"ordinary","family":"authority"}`, "v")
	require.NoError(t, err)
	t.Cleanup(func() { _ = nativeoperation.ReleaseOwner(t.Context(), claimed.Owner) })
	return claimed.Owner
}

func TestUpdateUserColsFencesAuthorityWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user.IsAdmin = !user.IsAdmin
	require.ErrorIs(t, user_model.UpdateUserCols(ctx, user, "is_admin"), nativeoperation.ErrBusy)

	fresh := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.Equal(t, !user.IsAdmin, fresh.IsAdmin)
}

func TestUpdateUserColsTelemetryProceedsWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.NoError(t, user_model.UpdateUserCols(ctx, &user_model.User{ID: user.ID, Theme: "fence-test-theme"}, "theme"))
}

func TestCreateUserFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.ErrorIs(t, user_model.CreateUser(ctx, &user_model.User{Name: "fence-test-user", Email: "fence-test-user@example.com"}), nativeoperation.ErrBusy)
	unittest.AssertNotExistsBean(t, &user_model.User{Name: "fence-test-user"})
}

func TestEmailWritesFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	_, err := user_model.InsertEmailAddress(ctx, &user_model.EmailAddress{UID: 2, Email: "fence-test@example.com"})
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
	require.ErrorIs(t, user_model.ActivateUserEmail(ctx, 2, "fence-test@example.com", true), nativeoperation.ErrBusy)
}

func TestFixWrongUserTypeFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	_, err := user_model.FixWrongUserType(ctx)
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
}
