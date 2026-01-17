// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package invited

import (
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

func Test(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	require.NotNil(t, t.Context())
	user1, err := user.GetUserByID(t.Context(), 1)
	require.NoError(t, err)
	user2, err := user.GetUserByID(t.Context(), 2)
	require.NoError(t, err)
	user3, err := user.GetUserByID(t.Context(), 3)
	require.NoError(t, err)
	user4, err := user.GetUserByID(t.Context(), 4)
	require.NoError(t, err)

	err = Log(t.Context(), user1.ID, user2.ID, "token")
	require.NoError(t, err)
	count, err := Since(t.Context(), user1.ID, 3600)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	err = Log(t.Context(), user2.ID, user3.ID, "token")
	require.NoError(t, err)
	count, err = Since(t.Context(), user2.ID, 3600)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	err = Log(t.Context(), user4.ID, user3.ID, "token")
	require.ErrorContains(t, err, "UNIQUE constraint failed: invitation.invitee")
	err = Log(t.Context(), user1.ID, user1.ID, "token")
	require.ErrorContains(t, err, "A user cannot invite themselves")
	err = Log(t.Context(), user3.ID, user1.ID, "token")
	require.ErrorContains(t, err, "(loop)")
	err = Log(t.Context(), user3.ID, user2.ID, "token")
	require.ErrorContains(t, err, "(loop)")
}
