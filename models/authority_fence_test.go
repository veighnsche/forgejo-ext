// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package models

import (
	"testing"

	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/models/organization"
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

func TestAddTeamMemberFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	team := unittest.AssertExistsAndLoadBean(t, &organization.Team{ID: 1})
	owner := claimTestOwner(t)

	require.ErrorIs(t, AddTeamMember(ctx, team, 4), nativeoperation.ErrBusy)
	unittest.AssertNotExistsBean(t, &organization.TeamUser{UID: 4, TeamID: 1})

	require.NoError(t, nativeoperation.ReleaseOwner(ctx, owner))
	require.NoError(t, AddTeamMember(ctx, team, 4))
	unittest.AssertExistsAndLoadBean(t, &organization.TeamUser{UID: 4, TeamID: 1})
}

func TestRemoveTeamMemberFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	team := unittest.AssertExistsAndLoadBean(t, &organization.Team{ID: 2})
	require.ErrorIs(t, RemoveTeamMember(ctx, team, 2), nativeoperation.ErrBusy)
	unittest.AssertExistsAndLoadBean(t, &organization.TeamUser{UID: 2, TeamID: 2})
}

func TestRemoveOrgUserFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.ErrorIs(t, RemoveOrgUser(ctx, 3, 4), nativeoperation.ErrBusy)
	unittest.AssertExistsAndLoadBean(t, &organization.OrgUser{OrgID: 3, UID: 4})
}

func TestDeleteDeployKeyFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.ErrorIs(t, DeleteDeployKey(ctx, 1, 1), nativeoperation.ErrBusy)
}
