package context

import (
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

func TestHasProjectPermission(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	// user1 is site admin
	user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	// user2 is writer in org3 and owner of repo1, not owner of repo3
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	org3 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3})
	// user11 is member of org17 and in team with read perms
	user11 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 11})
	org17 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 17})

	// Positive Cases
	// user2 -> user2
	hasPerm, err := hasWriteProjectPermission(t.Context(), user2, user2)
	require.NoError(t, err)
	assert.True(t, hasPerm)

	// Site Admin (user1) -> org3
	hasPerm, err = hasWriteProjectPermission(t.Context(), user1, org3)
	require.NoError(t, err)
	assert.True(t, hasPerm)

	// Org Writer (user2) -> org3
	hasPerm, err = hasWriteProjectPermission(t.Context(), user2, org3)
	require.NoError(t, err)
	assert.True(t, hasPerm)

	// User2 -> org17 (not member)
	hasPerm, err = hasWriteProjectPermission(t.Context(), user2, org17)
	require.NoError(t, err)
	assert.False(t, hasPerm)

	// Team Member with low perms (user11) -> org repo (low permissions)
	hasPerm, err = hasWriteProjectPermission(t.Context(), user11, org17)
	require.NoError(t, err)
	assert.False(t, hasPerm)
}
