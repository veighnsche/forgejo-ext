// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git_test

import (
	"testing"

	git_model "forgejo.org/models/git"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/require"
)

func snapshotProtectedBranch() *git_model.ProtectedBranch {
	return &git_model.ProtectedBranch{
		ID:                            11,
		RepoID:                        3,
		RuleName:                      "master",
		CanPush:                       false,
		EnableWhitelist:               true,
		WhitelistUserIDs:              []int64{2},
		WhitelistTeamIDs:              []int64{4},
		EnableMergeWhitelist:          true,
		WhitelistDeployKeys:           false,
		MergeWhitelistUserIDs:         []int64{2},
		MergeWhitelistTeamIDs:         nil,
		EnableStatusCheck:             true,
		StatusCheckContexts:           []string{"verify.yaml"},
		EnableApprovalsWhitelist:      false,
		ApprovalsWhitelistUserIDs:     nil,
		ApprovalsWhitelistTeamIDs:     nil,
		RequiredApprovals:             1,
		BlockOnRejectedReviews:        true,
		BlockOnOfficialReviewRequests: false,
		BlockOnOutdatedBranch:         true,
		DismissStaleApprovals:         false,
		IgnoreStaleApprovals:          false,
		RequireSignedCommits:          false,
		ProtectedFilePatterns:         "release/*",
		UnprotectedFilePatterns:       "",
		ApplyToAdmins:                 true,
		CreatedUnix:                   timeutil.TimeStamp(1700000000),
		UpdatedUnix:                   timeutil.TimeStamp(1700000050),
	}
}

func TestProtectionSnapshotPreservesMergePolicy(t *testing.T) {
	snap, err := git_model.NewProtectionSnapshot(snapshotProtectedBranch(), "master", true, "", true)
	require.NoError(t, err)
	require.True(t, snap.Visible)
	require.True(t, snap.Complete)
	require.Equal(t, int64(11), snap.RuleID)
	require.Equal(t, int64(3), snap.RepositoryID)
	require.Equal(t, "master", snap.RuleName)
	require.Equal(t, "master", snap.MatchedBranch)
	require.True(t, snap.EnableMergeWhitelist)
	require.Equal(t, []int64{2}, snap.MergeWhitelistUserIDs)
	require.True(t, snap.EnableStatusCheck)
	require.Equal(t, []string{"verify.yaml"}, snap.StatusCheckContexts)
	require.Equal(t, int64(1), snap.RequiredApprovals)
	require.True(t, snap.BlockOnRejectedReviews)
	require.True(t, snap.BlockOnOutdatedBranch)
	require.Equal(t, "release/*", snap.ProtectedFilePatterns)
	require.True(t, snap.ApplyToAdmins)
	require.Equal(t, int64(1700000050), snap.UpdatedUnix)

	hidden, err := git_model.NewProtectionSnapshot(snapshotProtectedBranch(), "master", false, git_model.CheckHiddenNoAccess, true)
	require.NoError(t, err)
	require.False(t, hidden.Visible)
	require.Equal(t, int64(11), hidden.RuleID)
	require.Equal(t, int64(3), hidden.RepositoryID)
	require.Equal(t, "master", hidden.RuleName)
	require.Empty(t, hidden.MatchedBranch)
	require.Empty(t, hidden.StatusCheckContexts)
	require.Empty(t, hidden.MergeWhitelistUserIDs)
	require.Zero(t, hidden.RequiredApprovals)

	bad := snapshotProtectedBranch()
	bad.RequiredApprovals = -1
	_, err = git_model.NewProtectionSnapshot(bad, "master", true, "", true)
	require.ErrorIs(t, err, git_model.ErrCheckSnapshotInvalid, "negative approvals must refuse")

	bad = snapshotProtectedBranch()
	bad.MergeWhitelistUserIDs = []int64{0}
	_, err = git_model.NewProtectionSnapshot(bad, "master", true, "", true)
	require.ErrorIs(t, err, git_model.ErrCheckSnapshotInvalid, "zero user id must refuse")

	_, err = git_model.NewProtectionSnapshot(snapshotProtectedBranch(), "ma ster", true, "", true)
	require.ErrorIs(t, err, git_model.ErrCheckSnapshotInvalid, "malformed branch must refuse")

	_, err = git_model.NewProtectionSnapshot(snapshotProtectedBranch(), "master", false, "bogus", true)
	require.ErrorIs(t, err, git_model.ErrCheckSnapshotInvalid, "unknown hidden reason must refuse")
}
