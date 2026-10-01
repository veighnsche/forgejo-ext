// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issue_test

import (
	"testing"

	issues_model "forgejo.org/models/issues"
	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	issue_service "forgejo.org/services/issue"

	"github.com/stretchr/testify/require"
)

// TestIssueClaimsRefuseWhileHeld proves the issue-service collaboration
// claims are wired: a title change refuses before any effect while a
// foreign owner holds the reservation, and proceeds when idle.
func TestIssueClaimsRefuseWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	claimed, err := nativeoperation.ClaimOrdinary(ctx, "ord:branch-delete/1/x", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	defer func() { _ = nativeoperation.ReleaseOwner(ctx, claimed.Owner) }()

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.ErrorIs(t, issue_service.ChangeTitle(ctx, issue, doer, "claimed title"), nativeoperation.ErrBusy)

	fresh := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	require.NotEqual(t, "claimed title", fresh.Title)
}

func TestIssueClaimsProceedWhenIdle(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.NoError(t, issue_service.ChangeTitle(ctx, issue, doer, "claimed title"))

	updated := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	require.Equal(t, "claimed title", updated.Title)
}
