// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package access_test

import (
	"fmt"
	"testing"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var crossRepoTargetID int64 = 9100

// insertActionsTarget creates a target repo (with code + actions units) owned by
// ownerID, with the given visibility and Actions access scope.
func insertActionsTarget(t *testing.T, ownerID int64, private bool, scope repo_model.ActionsAccessScope) *repo_model.Repository {
	t.Helper()
	crossRepoTargetID++
	repo := &repo_model.Repository{
		ID:        crossRepoTargetID,
		OwnerID:   ownerID,
		LowerName: fmt.Sprintf("cross-repo-%d", crossRepoTargetID),
		Name:      fmt.Sprintf("cross-repo-%d", crossRepoTargetID),
		IsPrivate: private,
	}
	unittest.AssertSuccessfulInsert(t, repo)
	unittest.AssertSuccessfulInsert(t, &repo_model.RepoUnit{RepoID: repo.ID, Type: unit.TypeCode})
	unittest.AssertSuccessfulInsert(t, &repo_model.RepoUnit{
		RepoID: repo.ID,
		Type:   unit.TypeActions,
		Config: &repo_model.ActionsConfig{AccessScope: scope},
	})
	return repo
}

// TestActionsAccessGrantsRead exercises every branch of the cross-repo Actions
// access predicate. The consuming job is owned by user 1, who is a member of
// org 35 but not of org 3 (per the shared fixtures).
func TestActionsAccessGrantsRead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	task := &actions_model.ActionTask{OwnerID: 1}

	t.Run("disabled instance-wide", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Actions.CrossRepoAccessEnabled, false)()
		target := insertActionsTarget(t, 1, true, repo_model.ActionsAccessScopeSameOwner)
		assert.False(t, access.ActionsAccessGrantsRead(db.DefaultContext, target, task))
	})

	defer test.MockVariableValue(&setting.Actions.CrossRepoAccessEnabled, true)()

	t.Run("public target is a no-op", func(t *testing.T) {
		target := insertActionsTarget(t, 1, false, repo_model.ActionsAccessScopeSameOwner)
		assert.False(t, access.ActionsAccessGrantsRead(db.DefaultContext, target, task))
	})

	t.Run("scope none denies", func(t *testing.T) {
		target := insertActionsTarget(t, 1, true, repo_model.ActionsAccessScopeNone)
		assert.False(t, access.ActionsAccessGrantsRead(db.DefaultContext, target, task))
	})

	t.Run("same-owner grants for the same owner", func(t *testing.T) {
		target := insertActionsTarget(t, 1, true, repo_model.ActionsAccessScopeSameOwner)
		assert.True(t, access.ActionsAccessGrantsRead(db.DefaultContext, target, task))
	})

	t.Run("same-owner denies a different owner", func(t *testing.T) {
		target := insertActionsTarget(t, 2, true, repo_model.ActionsAccessScopeSameOwner)
		assert.False(t, access.ActionsAccessGrantsRead(db.DefaultContext, target, task))
	})

	t.Run("same-org grants an org member", func(t *testing.T) {
		target := insertActionsTarget(t, 35, true, repo_model.ActionsAccessScopeSameOrg)
		assert.True(t, access.ActionsAccessGrantsRead(db.DefaultContext, target, task))
	})

	t.Run("same-org denies a non-member", func(t *testing.T) {
		target := insertActionsTarget(t, 3, true, repo_model.ActionsAccessScopeSameOrg)
		assert.False(t, access.ActionsAccessGrantsRead(db.DefaultContext, target, task))
	})

	t.Run("same-org denies when the owner is not an org", func(t *testing.T) {
		target := insertActionsTarget(t, 2, true, repo_model.ActionsAccessScopeSameOrg)
		assert.False(t, access.ActionsAccessGrantsRead(db.DefaultContext, target, task))
	})
}

// TestGetActionRepoPermissionCrossRepo checks that the grant flows through
// GetActionRepoPermission as a read-only permission over the target's units.
func TestGetActionRepoPermissionCrossRepo(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Actions.CrossRepoAccessEnabled, true)()

	// A job owned by user 1 (member of org 35), running in an unrelated repo.
	task := &actions_model.ActionTask{OwnerID: 1, RepoID: 987654}

	t.Run("granted target is read-only over its units", func(t *testing.T) {
		target := insertActionsTarget(t, 35, true, repo_model.ActionsAccessScopeSameOrg)
		perm, err := access.GetActionRepoPermission(db.DefaultContext, target, task)
		require.NoError(t, err)
		assert.Equal(t, perm_model.AccessModeRead, perm.AccessMode)
		assert.True(t, perm.CanRead(unit.TypeCode))
		assert.False(t, perm.CanWrite(unit.TypeCode))
	})

	t.Run("target without opt-in stays denied", func(t *testing.T) {
		target := insertActionsTarget(t, 35, true, repo_model.ActionsAccessScopeNone)
		perm, err := access.GetActionRepoPermission(db.DefaultContext, target, task)
		require.NoError(t, err)
		assert.Equal(t, perm_model.AccessModeNone, perm.AccessMode)
		assert.False(t, perm.CanRead(unit.TypeCode))
	})
}
