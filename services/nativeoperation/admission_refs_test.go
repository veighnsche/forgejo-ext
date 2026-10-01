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
