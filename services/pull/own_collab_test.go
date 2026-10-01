// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull_test

import (
	"encoding/json"
	"testing"

	operation_service "forgejo.org/services/nativeoperation"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/require"
)

// TestCollabOwnershipWireContract pins the pull-local collaboration
// mirror to the native-operation wire shape: the same scope JSON field
// names the service scope decodes, and resource labels identical to the
// canonical builders, so offline recovery reads pull claims exactly
// like service claims. The mirror exists only to break the pull to
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

	require.Equal(t, operation_service.PullResource(3, "retarget"), pull_service.CollabPullResource(3, "retarget"))
	require.Equal(t, operation_service.PullCreateResource(7), pull_service.CollabPullCreateResource(7))
	require.Equal(t, operation_service.ReviewResource(5, "dismiss"), pull_service.CollabReviewResource(5, "dismiss"))
	require.Equal(t, operation_service.ReviewSubmitResource(7), pull_service.CollabReviewSubmitResource(7))
	require.Equal(t, operation_service.CommentCreateResource(7), pull_service.CollabCommentCreateResource(7))
	require.Equal(t, operation_service.IssueResource(7, "title"), pull_service.CollabIssueResource(7, "title"))
	require.Equal(t, operation_service.CollabBatchResource("pr-sweep"), pull_service.CollabBatchResource("pr-sweep"))
}
