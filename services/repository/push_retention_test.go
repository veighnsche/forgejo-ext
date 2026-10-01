// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"testing"

	"forgejo.org/models/db"
	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"
	repo_module "forgejo.org/modules/repository"

	"github.com/stretchr/testify/require"
)

func TestPushHandlerRetainsBusyWork(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	_, err := nativeoperation.ClaimOrdinary(db.DefaultContext, "ord:branch-delete/1/branch2", `{"kind":"ordinary"}`, "v")
	require.NoError(t, err)

	opts := []*repo_module.PushUpdateOptions{{
		RefFullName:  git.RefNameFromBranch("master"),
		OldCommitID:  "65f1bf27bc3bf70f64657658635e66094edbcb4d",
		NewCommitID:  "985f0301dba5e7b34be866819cd15ad3d8f508ee",
		PusherID:     2,
		PusherName:   "user2",
		RepoUserName: "user2",
		RepoName:     "repo1",
	}}
	unhandled := handler(opts)
	require.Len(t, unhandled, 1)
	require.Equal(t, opts, unhandled[0])

	// The retained batch is not consumed: the owner still holds it.
	held, err := nativeoperation.ReadReservation(db.DefaultContext)
	require.NoError(t, err)
	require.Equal(t, "ord:branch-delete/1/branch2", held.Owner)
}
