// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"errors"
	"testing"

	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExactMergeRefusalContract locks the refusal type, predicate and reason
// vocabulary the conditional submit draft maps to SDK refusal reasons.
// Behavioral paths need live Git plus the post-receive bookkeeping server and
// are covered by the exact-merge integration test.
func TestExactMergeRefusalContract(t *testing.T) {
	reasons := []string{
		ExactMergeRefusedMalformedRef,
		ExactMergeRefusedMalformedOID,
		ExactMergeRefusedCrossRepo,
		ExactMergeRefusedPRMismatch,
		ExactMergeRefusedClosedOrMerged,
		ExactMergeRefusedMissingHead,
		ExactMergeRefusedMissingBase,
		ExactMergeRefusedStaleHead,
		ExactMergeRefusedStaleBase,
		ExactMergeRefusedNoOp,
		ExactMergeRefusedMethodNotAllowed,
		ExactMergeRefusedResultMismatch,
	}
	seen := map[string]bool{}
	for _, reason := range reasons {
		require.NotEmpty(t, reason)
		require.False(t, seen[reason], "refusal reason %q must be distinct", reason)
		seen[reason] = true
		err := ErrExactMergeRefused{Reason: reason}
		require.ErrorContains(t, err, reason)
		require.True(t, IsErrExactMergeRefused(err))
		require.True(t, errors.As(err, &ErrExactMergeRefused{}))
	}
	require.False(t, IsErrExactMergeRefused(nil))
	require.False(t, IsErrExactMergeRefused(errors.New("boom")))
	require.False(t, IsErrExactMergeRefused(ErrIsClosed))

	assert.True(t, isExactMergeBranchRef("refs/heads/master"))
	assert.True(t, isExactMergeBranchRef("refs/heads/feature/x"))
	assert.False(t, isExactMergeBranchRef(""))
	assert.False(t, isExactMergeBranchRef("master"))
	assert.False(t, isExactMergeBranchRef("refs/pull/1/head"))
	assert.False(t, isExactMergeBranchRef("refs/heads/ma ster"))
	assert.False(t, isExactMergeBranchRef("refs/heads/a..b"))
}

func TestExactMergeRefusesWithoutLiveState(t *testing.T) {
	ctx := t.Context()
	doer := &user_model.User{}

	_, err := MergeExactFastForward(ctx, nil, doer, nil, "refs/heads/h", "refs/heads/b", "", "", "")
	require.Error(t, err)
	assert.True(t, IsErrExactMergeRefused(err))
	assert.Equal(t, ExactMergeRefusedPRMismatch, err.(ErrExactMergeRefused).Reason)

	pr := &issues_model.PullRequest{}
	_, err = MergeExactFastForward(ctx, pr, doer, nil, "refs/heads/h", "refs/heads/b", "", "", "")
	require.Error(t, err)
	assert.True(t, IsErrExactMergeRefused(err))

	unopened := &git.Repository{}
	_, err = MergeExactFastForward(ctx, pr, doer, unopened, "h", "refs/heads/b", "", "", "")
	require.Error(t, err)
	assert.Equal(t, ExactMergeRefusedMalformedRef, err.(ErrExactMergeRefused).Reason)

	_, err = MergeExactFastForward(ctx, pr, doer, unopened, "refs/heads/h", "refs/pull/1/head", "", "", "")
	require.Error(t, err)
	assert.Equal(t, ExactMergeRefusedMalformedRef, err.(ErrExactMergeRefused).Reason)
}
