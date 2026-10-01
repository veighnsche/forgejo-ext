// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"testing"
	"time"

	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	execcontext "forgejo.org/modules/nativeoperation"

	"github.com/stretchr/testify/require"
)

// TestClassifyOrdinaryOwnerMultiRefScope pins the multi-ref ordinary
// gate: a proven ref listed in the scope's ref batch is admitted, an
// unlisted ref refuses, and a batch scope with no single ref admits
// exactly its members. Single-ref verdicts are covered by
// TestClassifyOrdinaryOwnerScope and must not change.
func TestClassifyOrdinaryOwnerMultiRefScope(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := admissionService(t, nil, time.Now().Unix())
	encoded, err := encodeScope(Scope{
		Kind:         model.OwnerOrdinary,
		Family:       FamilyReceiveHTTP,
		RepositoryID: 1,
		Refs: []ScopedRef{
			{Ref: "refs/heads/one", OldOID: testBase, NewOID: testHead},
			{Ref: "refs/heads/two", OldOID: testBase, NewOID: testHead},
		},
	})
	require.NoError(t, err)
	owner := "ord:receive-http/1/main/z"
	_, err = model.ClaimOrdinary(ctx, owner, encoded, execcontext.Verifier("receive-proof"))
	require.NoError(t, err)
	defer func() { require.NoError(t, model.ReleaseOwner(ctx, owner)) }()

	allowed, err := svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{
			{Old: testBase, New: testHead, Ref: "refs/heads/one"},
			{Old: testBase, New: testHead, Ref: "refs/heads/two"},
		},
		Proof: "receive-proof",
	})
	require.NoError(t, err)
	require.True(t, allowed.Allowed)

	denied, err := svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/other"}},
		Proof: "receive-proof",
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed)

	completed, err := svc.ClassifyCompletion(ctx, 1, []string{"refs/heads/one", "refs/heads/two"}, "receive-proof", false)
	require.NoError(t, err)
	require.True(t, completed.Allowed)

	completedDenied, err := svc.ClassifyCompletion(ctx, 1, []string{"refs/heads/other"}, "receive-proof", false)
	require.NoError(t, err)
	require.False(t, completedDenied.Allowed)
}

// TestCollabPullCreateRefinesPRRef pins the pull-creation gate: the
// derived PR ref is unknowable at claim time, so the hook-time
// declaration admits the operation's own ref once refined, while a
// non-PR ref under the same owner still refuses.
func TestCollabPullCreateRefinesPRRef(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := admissionService(t, nil, time.Now().Unix())
	encoded, err := encodeScope(Scope{
		Kind:         model.OwnerOrdinary,
		Family:       FamilyCollaboration,
		RepositoryID: 1,
	})
	require.NoError(t, err)
	owner := "ord:collaboration/pull/new/1/abcdef0123456789"
	_, err = model.ClaimOrdinary(ctx, owner, encoded, execcontext.Verifier("pull-proof"))
	require.NoError(t, err)
	defer func() { require.NoError(t, model.ReleaseOwner(ctx, owner)) }()

	classify := func(ref string) TransactionDecision {
		decision, err := svc.ClassifyTransaction(ctx, TransactionRequest{
			OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
			Lines: []RefLine{{Old: "0000000000000000000000000000000000000000", New: testHead, Ref: ref}},
			Proof: "pull-proof",
		})
		require.NoError(t, err)
		return decision
	}

	denied := classify("refs/pull/1/head")
	require.False(t, denied.Allowed)

	RefineCollabPullScope(ctx, "pull-proof", []ScopedRef{{Ref: "refs/pull/1/head", NewOID: testHead}})
	allowed := classify("refs/pull/1/head")
	require.True(t, allowed.Allowed, "refused: %s", allowed.Reason)

	RefineCollabPullScope(ctx, "pull-proof", []ScopedRef{{Ref: "refs/heads/main", NewOID: testHead}})
	stillDenied := classify("refs/heads/main")
	require.False(t, stillDenied.Allowed)
}

// TestCollabPullRefinementNoOps pins the refinement boundaries: forged
// proofs, other collaboration operations and other families never
// refine, so their ref pushes keep the existing strict checking.
func TestCollabPullRefinementNoOps(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := admissionService(t, nil, time.Now().Unix())

	claim := func(owner string) {
		encoded, err := encodeScope(Scope{
			Kind:         model.OwnerOrdinary,
			Family:       FamilyCollaboration,
			RepositoryID: 1,
		})
		require.NoError(t, err)
		_, err = model.ClaimOrdinary(ctx, owner, encoded, execcontext.Verifier("pull-proof"))
		require.NoError(t, err)
	}
	classify := func() TransactionDecision {
		decision, err := svc.ClassifyTransaction(ctx, TransactionRequest{
			OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
			Lines: []RefLine{{Old: "0000000000000000000000000000000000000000", New: testHead, Ref: "refs/pull/1/head"}},
			Proof: "pull-proof",
		})
		require.NoError(t, err)
		return decision
	}

	claim("ord:collaboration/pull/new/1/abcdef0123456789")
	RefineCollabPullScope(ctx, "forged-proof", []ScopedRef{{Ref: "refs/pull/1/head", NewOID: testHead}})
	require.False(t, classify().Allowed)
	require.NoError(t, model.ReleaseOwner(ctx, "ord:collaboration/pull/new/1/abcdef0123456789"))

	claim("ord:collaboration/issue/1/title/abcdef0123456789")
	RefineCollabPullScope(ctx, "pull-proof", []ScopedRef{{Ref: "refs/pull/1/head", NewOID: testHead}})
	require.False(t, classify().Allowed)
	require.NoError(t, model.ReleaseOwner(ctx, "ord:collaboration/issue/1/title/abcdef0123456789"))
}
