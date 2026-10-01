// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCollabResource(t *testing.T) {
	cases := []struct {
		resource string
		kind     string
		id, id2  int64
		op       string
		ok       bool
	}{
		{IssueResource(7, "title"), "issue", 7, 0, "title", true},
		{IssueResource(7, "content"), "issue", 7, 0, "content", true},
		{IssueResource(7, "status"), "issue", 7, 0, "status", true},
		{IssueResource(7, "delete"), "issue", 7, 0, "delete", true},
		{IssueCreateResource(1), "issue", 1, 0, "create", true},
		{CommentResource(9, "update"), "comment", 9, 0, "update", true},
		{CommentResource(9, "conversation"), "comment", 9, 0, "conversation", true},
		{CommentResource(9, "attachment"), "comment", 9, 0, "attachment", true},
		{CommentCreateResource(7), "comment", 7, 0, "create", true},
		{DependencyResource(7, 8), "dependency", 7, 8, "", true},
		{PullResource(3, "retarget"), "pull", 3, 0, "retarget", true},
		{PullResource(3, "automerge"), "pull", 3, 0, "automerge", true},
		{PullResource(3, "manual"), "pull", 3, 0, "manual", true},
		{PullResource(3, "edits"), "pull", 3, 0, "edits", true},
		{PullCreateResource(7), "pull", 7, 0, "create", true},
		{ReviewResource(5, "dismiss"), "review", 5, 0, "dismiss", true},
		{ReviewRequestResource(7, 2), "review", 7, 2, "request-user", true},
		{ReviewTeamRequestResource(7, 3), "review", 7, 3, "request-team", true},
		{ReviewSubmitResource(7), "review", 7, 0, "submit", true},
		{ReviewDeleteResource(7, 5), "review", 7, 5, "delete", true},
		{ReactionResource(7, 0, 2), "reaction", 7, 0, "2", true},
		{ReactionResource(7, 9, 2), "reaction", 7, 9, "2", true},
		{LabelResource(4, "update"), "label", 4, 0, "update", true},
		{LabelResource(1, "create"), "label", 1, 0, "create", true},
		{MilestoneResource(6, "status"), "milestone", 6, 0, "status", true},
		{MilestoneResource(1, "create"), "milestone", 1, 0, "create", true},
		{HistoryResource(11), "history", 11, 0, "delete", true},
		{CollabBatchResource("orphan-fix"), "batch", 0, 0, "orphan-fix", true},
		{CollabBatchResource("push-commit"), "batch", 0, 0, "push-commit", true},
		{CollabBatchResource("pr-sweep"), "batch", 0, 0, "pr-sweep", true},
		{"issue/7/approve", "", 0, 0, "", false},
		{"issue/0/title", "", 0, 0, "", false},
		{"issue/seven/title", "", 0, 0, "", false},
		{"issue/7", "", 0, 0, "", false},
		{"comment/9/publish", "", 0, 0, "", false},
		{"comment/new/0", "", 0, 0, "", false},
		{"dependency/7", "", 0, 0, "", false},
		{"dependency/7/0", "", 0, 0, "", false},
		{"pull/3/merge", "", 0, 0, "", false},
		{"review/5/approve", "", 0, 0, "", false},
		{"review/5/request", "", 0, 0, "", false},
		{"review/7/5/close", "", 0, 0, "", false},
		{"review/7/request-user/0", "", 0, 0, "", false},
		{"reaction/7/0", "", 0, 0, "", false},
		{"reaction/7/-1/2", "", 0, 0, "", false},
		{"label/4/rename", "", 0, 0, "", false},
		{"milestone/6/close", "", 0, 0, "", false},
		{"history/11/soft", "", 0, 0, "", false},
		{"batch/something", "", 0, 0, "", false},
		{"watch/7", "", 0, 0, "", false},
		{"", "", 0, 0, "", false},
	}
	for _, tc := range cases {
		claim, err := parseCollabResource(tc.resource)
		require.Equal(t, tc.ok, err == nil, "resource %q", tc.resource)
		if !tc.ok {
			continue
		}
		require.Equal(t, tc.kind, claim.kind, "resource %q", tc.resource)
		require.Equal(t, tc.id, claim.id, "resource %q", tc.resource)
		require.Equal(t, tc.id2, claim.id2, "resource %q", tc.resource)
		require.Equal(t, tc.op, claim.op, "resource %q", tc.resource)
	}
}
