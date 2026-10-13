// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"testing"
	"time"

	git_model "forgejo.org/models/git"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/require"
)

func TestSyncBranchesToDBReadsCurrentTip(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.AppDataPath, t.TempDir())()
	branch := unittest.AssertExistsAndLoadBean(t, &git_model.Branch{ID: 4})
	currentCommit := &git.Commit{
		ID:            git.MustIDFromString("985f0301dba5e7b34be866819cd15ad3d8f508ee"),
		CommitMessage: "current tip",
		Committer:     &git.Signature{When: time.Unix(1579166279, 0)},
	}
	getCurrentTip := func(name string) (*git.Commit, error) {
		require.Equal(t, branch.Name, name)
		return currentCommit, nil
	}

	// The same branch notification may be processed again after Git has
	// advanced. Each sync resolves the current tip rather than replaying
	// an earlier hook's commit ID into the database.
	for _, oid := range []string{
		"985f0301dba5e7b34be866819cd15ad3d8f508ee",
		"62fb502a7172d4453f0322a2cc85bddffa57f07a",
	} {
		currentCommit.ID = git.MustIDFromString(oid)
		require.NoError(t, SyncBranchesToDB(t.Context(), branch.RepoID, branch.PusherID, []string{branch.Name}, getCurrentTip))
		updated, err := git_model.GetBranch(t.Context(), branch.RepoID, branch.Name)
		require.NoError(t, err)
		require.Equal(t, oid, updated.CommitID)
	}
}
