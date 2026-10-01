// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	operation_service "forgejo.org/services/nativeoperation"

	"github.com/stretchr/testify/require"
)

func TestUpdateUserTelemetryDoesNotClaim(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	before, err := operation_service.Default().ReadNativeRevision(ctx)
	require.NoError(t, err)

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 28})
	require.NoError(t, UpdateUser(ctx, user, &UpdateOptions{
		Language:      optional.Some("en-US"),
		DiffViewStyle: optional.Some("unified"),
		SetLastLogin:  true,
	}))

	after, err := operation_service.Default().ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision)
	require.True(t, after.Idle)
}

func TestUpdateUserAuthorityClaimsAndReleases(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	before, err := operation_service.Default().ReadNativeRevision(ctx)
	require.NoError(t, err)

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 28})
	require.NoError(t, UpdateUser(ctx, user, &UpdateOptions{
		FullName: optional.Some("Authority Claim Test"),
		IsActive: optional.Some(true),
	}))

	after, err := operation_service.Default().ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.Greater(t, after.Revision, before.Revision)
	require.True(t, after.Idle)
}
