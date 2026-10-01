// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull_test

import (
	"testing"

	nativeoperation "forgejo.org/models/nativeoperation"
	pull_model "forgejo.org/models/pull"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/require"
)

func TestAutomergeWritersFencesWhileHeld(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	claimed, err := nativeoperation.ClaimOrdinary(ctx, "ord:collaboration/test/abc123", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)
	defer func() { _ = nativeoperation.ReleaseOwner(ctx, claimed.Owner) }()

	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.ErrorIs(t, pull_model.ScheduleAutoMerge(ctx, doer, 1, repo_model.MergeStyleMerge, "", false), nativeoperation.ErrBusy)
	require.ErrorIs(t, pull_model.DeleteScheduledAutoMerge(ctx, 1), nativeoperation.ErrBusy)
}
