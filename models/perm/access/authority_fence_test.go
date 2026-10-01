// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package access_test

import (
	"testing"

	nativeoperation "forgejo.org/models/nativeoperation"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
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

func TestRecalculateAccessesFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	claimTestOwner(t)

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3})
	require.ErrorIs(t, access_model.RecalculateAccesses(ctx, repo), nativeoperation.ErrBusy)
	require.ErrorIs(t, access_model.RecalculateUserAccess(ctx, repo, 2), nativeoperation.ErrBusy)
	require.ErrorIs(t, access_model.RecalculateTeamAccesses(ctx, repo, 0), nativeoperation.ErrBusy)
	require.ErrorIs(t, access_model.RecalculateUserAccessForRepos(ctx, 2, []int64{3}), nativeoperation.ErrBusy)
}
