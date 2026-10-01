// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package convert

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToPullReviewCommentListDeterministicOrder(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 2})
	require.NoError(t, issue.LoadRepo(ctx))
	review := unittest.AssertExistsAndLoadBean(t, &issues_model.Review{ID: 4})
	review.Issue = issue
	require.NoError(t, review.LoadCodeComments(ctx))
	require.NotEmpty(t, review.CodeComments)
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

	first, err := ToPullReviewCommentList(ctx, review, doer)
	require.NoError(t, err)
	require.NotEmpty(t, first)

	// The code comments live in a nested map; the API list must still come
	// out in one stable order on every call.
	for range 5 {
		next, err := ToPullReviewCommentList(ctx, review, doer)
		require.NoError(t, err)
		require.Len(t, next, len(first))
		for i := range first {
			assert.Equal(t, first[i].ID, next[i].ID)
		}
	}

	// Order is tree path, then line, then comment id.
	for i := 1; i < len(first); i++ {
		prev, curr := first[i-1], first[i]
		prevLine := prev.LineNum
		if prev.OldLineNum > 0 {
			prevLine = -prev.OldLineNum
		}
		currLine := curr.LineNum
		if curr.OldLineNum > 0 {
			currLine = -curr.OldLineNum
		}
		if prev.Path != curr.Path {
			assert.Less(t, prev.Path, curr.Path)
		} else if prevLine != currLine {
			assert.Less(t, prevLine, currLine)
		} else {
			assert.Less(t, prev.ID, curr.ID)
		}
	}
}
