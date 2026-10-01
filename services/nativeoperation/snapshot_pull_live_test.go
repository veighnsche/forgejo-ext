// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"testing"

	sdk "forgejo.org/extension-sdk"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

// Live-row pull regression: xorm stamps pull_request.merged_unix on insert
// (the column carries the `updated` tag), so every live-created open PR has
// merged_unix set. The pull snapshot must not treat that auto-stamp as merge
// evidence: the read succeeds and reports no merge time.
func TestSnapshotReadPullToleratesLiveMergedUnixStamp(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()

	issue := insertSnapshotIssue(t, 1, 107, "Live candidate", "body", 0)
	pr := &issues_model.PullRequest{
		IssueID: issue.ID, Index: 107,
		HeadRepoID: 1, BaseRepoID: 1,
		HeadBranch: "branch2", BaseBranch: "master",
		Flow:   issues_model.PullRequestFlowGithub,
		Status: issues_model.PullRequestStatusMergeable,
	}
	unittest.AssertSuccessfulInsert(t, pr)
	stored, err := issues_model.GetPullRequestByIndex(ctx, 1, 107)
	require.NoError(t, err)
	require.NotZero(t, int64(stored.MergedUnix), "xorm must stamp merged_unix on insert like live rows")

	svc := NewService()
	req := snapshotProofRequest("1", "2", []string{sdk.SnapshotFamilyPull})
	req.PullNumber = "107"
	snapshot, err := svc.ReadSnapshot(ctx, 2, req)
	require.NoError(t, err)
	pull := snapshot.Pull
	require.True(t, pull.Visible)
	require.True(t, pull.Complete)
	require.Equal(t, "107", pull.Number)
	require.False(t, pull.HasMerged)
	require.Zero(t, pull.MergedUnix, "unmerged PRs report no merge time")
	require.Equal(t, snapshotBranch2, pull.HeadTip)
}
