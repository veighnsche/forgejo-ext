// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package organization_test

import (
	"testing"

	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/models/organization"
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

func TestCreateOrganizationFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.ErrorIs(t, organization.CreateOrganization(ctx, &organization.Organization{Name: "fence-test-org"}, owner), nativeoperation.ErrBusy)
	unittest.AssertNotExistsBean(t, &user_model.User{Name: "fence-test-org"})
}

func TestAddOrgUserFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.ErrorIs(t, organization.AddOrgUser(ctx, 3, 5), nativeoperation.ErrBusy)
}

func TestRemoveTeamRepoFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	require.ErrorIs(t, organization.RemoveTeamRepo(ctx, 1, 1), nativeoperation.ErrBusy)
}

func TestTeamInviteWritesFenceWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	team := unittest.AssertExistsAndLoadBean(t, &organization.Team{ID: 1})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	_, err := organization.CreateTeamInvite(ctx, doer, team, "fence-test@example.com")
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
	require.ErrorIs(t, organization.RemoveInviteByID(ctx, 1, 1), nativeoperation.ErrBusy)
}

func TestFixInconsistentOwnerTeamsFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	_, err := organization.FixInconsistentOwnerTeams(ctx)
	require.ErrorIs(t, err, nativeoperation.ErrBusy)
}
