// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issue_test

import (
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"testing"

	issue_service "forgejo.org/services/issue"
	nativeoperation_service "forgejo.org/services/nativeoperation"

	"github.com/stretchr/testify/require"
)

// TestCollabOwnershipWireContract pins the issue-local collaboration
// mirror to the native-operation wire shape: the same scope JSON field
// names the service scope decodes, and resource labels identical to the
// canonical builders, so offline recovery reads issue claims exactly
// like service claims. The mirror exists only to break the issue to
// native-operation import cycle; any drift here is a recovery bug.
func TestCollabOwnershipWireContract(t *testing.T) {
	raw, err := json.Marshal(struct {
		Kind         string `json:"kind"`
		Family       string `json:"family,omitempty"`
		RepositoryID int64  `json:"repository_id"`
	}{Kind: "ordinary", Family: "collaboration", RepositoryID: 7})
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, map[string]any{"kind": "ordinary", "family": "collaboration", "repository_id": float64(7)}, decoded)

	require.Equal(t, nativeoperation_service.IssueResource(7, "title"), issue_service.CollabIssueResource(7, "title"))
	require.Equal(t, nativeoperation_service.CommentResource(9, "update"), issue_service.CollabCommentResource(9, "update"))
	require.Equal(t, nativeoperation_service.CommentCreateResource(7), issue_service.CollabCommentCreateResource(7))
	require.Equal(t, nativeoperation_service.DependencyResource(7, 8), issue_service.CollabDependencyResource(7, 8))
	require.Equal(t, nativeoperation_service.ReviewSubmitResource(7), issue_service.CollabReviewSubmitResource(7))
	require.Equal(t, nativeoperation_service.ReactionResource(7, 0, 2), issue_service.CollabReactionResource(7, 0, 2))
	require.Equal(t, nativeoperation_service.CollabBatchResource("push-commit"), issue_service.CollabBatchResource("push-commit"))
	require.Equal(t, nativeoperation_service.IssueCreateResource(1), issue_service.CollabIssueCreateResource(1))
	require.Equal(t, nativeoperation_service.ReviewRequestResource(7, 2), issue_service.CollabReviewRequestResource(7, 2))
	require.Equal(t, nativeoperation_service.ReviewTeamRequestResource(7, 3), issue_service.CollabReviewTeamRequestResource(7, 3))
}
