// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"errors"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/extensionauth"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	execcontext "forgejo.org/modules/nativeoperation"

	"github.com/stretchr/testify/require"
)

type stubRefs struct {
	tips map[string]string
	err  error
}

func (s stubRefs) CommitID(_ context.Context, _ string, ref string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	tip, ok := s.tips[ref]
	if !ok {
		return "", errors.New("unknown ref")
	}
	return tip, nil
}

func admissionService(t *testing.T, tips map[string]string, now int64) *Service {
	t.Helper()
	return &Service{refs: stubRefs{tips: tips}, now: func() int64 { return now }, capabilityDir: t.TempDir()}
}

func writerToken(t *testing.T) *auth_model.AccessToken {
	t.Helper()
	token := &auth_model.AccessToken{
		UID:              2,
		Name:             "operation-test",
		Scope:            auth_model.AccessTokenScopeWriteRepository,
		ResourceAllRepos: true,
	}
	require.NoError(t, auth_model.NewAccessToken(t.Context(), token))
	_, err := extensionauth.EnrollBinding(t.Context(), operationTestInstallation, token.ID, 2, 1, extensionauth.KindMerge)
	require.NoError(t, err)
	return token
}

func claimMergeOwner(t *testing.T, ctx context.Context, token *auth_model.AccessToken, id string, scope Scope, now int64) (owner, secret string) {
	t.Helper()
	observation, err := NewService().ReadNativeRevision(ctx)
	require.NoError(t, err)
	secret = "proof-" + id
	owner = conditionalOwner(operationTestInstallation, id)
	op := &model.Operation{
		InstallationID:         operationTestInstallation,
		OperationID:            id,
		Kind:                   model.KindMerge,
		ActorID:                2,
		RepositoryID:           1,
		TokenID:                token.ID,
		CredentialFingerprint:  extensionauth.CredentialFingerprint(token.TokenHash, token.TokenSalt),
		AuthRevision:           "rev-1",
		ExpectedNativeRevision: observation.Revision,
		NotAfter:               now + 300,
		IntentDigest:           "digest-" + id,
		Intent:                 `{}`,
	}
	encoded, err := encodeScope(scope)
	require.NoError(t, err)
	_, err = model.ClaimConditional(ctx, op, owner, encoded, execcontext.Verifier(secret))
	require.NoError(t, err)
	return owner, secret
}

func mergeScope() Scope {
	return Scope{
		Kind:         model.OwnerConditional,
		RepositoryID: 1,
		Ref:          "refs/heads/master",
		OldOID:       testBase,
		NewOID:       testHead,
		HeadRef:      "refs/heads/branch2",
		HeadOID:      testHead,
		PRNumber:     3,
	}
}

func TestClassifyTransactionIdleAllows(t *testing.T) {
	unittest.PrepareTestEnv(t)
	svc := admissionService(t, nil, time.Now().Unix())
	decision, err := svc.ClassifyTransaction(t.Context(), TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/master"}},
	})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
}

func TestPreparedAdmissionHappyPath(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	now := time.Now().Unix()
	svc := admissionService(t, map[string]string{"refs/heads/branch2": testHead}, now)
	token := writerToken(t)
	_, secret := claimMergeOwner(t, ctx, token, "op-prepared", mergeScope(), now)

	decision, err := svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/master"}},
		Proof: secret,
	})
	require.NoError(t, err)
	require.True(t, decision.Allowed, "refused: %s", decision.Reason)

	op, err := model.LookupOperation(ctx, operationTestInstallation, "op-prepared")
	require.NoError(t, err)
	require.True(t, op.Admitted)
}

func TestPreparedAdmissionRefusals(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	now := time.Now().Unix()
	token := writerToken(t)

	// A forged execution proof cannot reenter the held owner.
	svc := admissionService(t, map[string]string{"refs/heads/branch2": testHead}, now)
	_, _ = claimMergeOwner(t, ctx, token, "op-forged", mergeScope(), now)
	decision, err := svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/master"}},
		Proof: "forged-proof",
	})
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	forged, err := model.LookupOperation(ctx, operationTestInstallation, "op-forged")
	require.NoError(t, err)
	require.False(t, forged.Admitted)
	require.NoError(t, model.ReleaseOwner(ctx, conditionalOwner(operationTestInstallation, "op-forged")))

	// A changed head refuses at the checkpoint.
	svc = admissionService(t, map[string]string{"refs/heads/branch2": testBase}, now)
	_, secret := claimMergeOwner(t, ctx, token, "op-stale-head", mergeScope(), now)
	decision, err = svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/master"}},
		Proof: secret,
	})
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, model.ReasonStaleHead, decision.Reason)
	recorded, err := model.LookupOperation(ctx, operationTestInstallation, "op-stale-head")
	require.NoError(t, err)
	require.False(t, recorded.Admitted)
	require.Equal(t, model.ReasonStaleHead, recorded.Reason)
	require.NoError(t, model.ReleaseOwner(ctx, conditionalOwner(operationTestInstallation, "op-stale-head")))

	// Cancellation winning before admission prevents the write.
	svc = admissionService(t, map[string]string{"refs/heads/branch2": testHead}, now)
	_, secret = claimMergeOwner(t, ctx, token, "op-cancel-first", mergeScope(), now)
	_, err = model.RevokeOperation(ctx, operationTestInstallation, "op-cancel-first")
	require.NoError(t, err)
	decision, err = svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/master"}},
		Proof: secret,
	})
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, model.ReasonCancelledBeforeAdmission, decision.Reason)
	require.NoError(t, model.ReleaseOwner(ctx, conditionalOwner(operationTestInstallation, "op-cancel-first")))

	// An elapsed admission deadline refuses.
	svc = admissionService(t, map[string]string{"refs/heads/branch2": testHead}, now+1000)
	_, secret = claimMergeOwner(t, ctx, token, "op-expired", mergeScope(), now)
	decision, err = svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/master"}},
		Proof: secret,
	})
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, model.ReasonExpiredBeforeAdmission, decision.Reason)
}

func TestPreparedAdmissionDuplicateAndCompletionPhases(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	now := time.Now().Unix()
	svc := admissionService(t, map[string]string{"refs/heads/branch2": testHead}, now)
	token := writerToken(t)
	_, secret := claimMergeOwner(t, ctx, token, "op-duplicate", mergeScope(), now)

	first, err := svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/master"}},
		Proof: secret,
	})
	require.NoError(t, err)
	require.True(t, first.Allowed)

	second, err := svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/master"}},
		Proof: secret,
	})
	require.NoError(t, err)
	require.False(t, second.Allowed)
	require.Equal(t, model.ReasonDuplicateAdmission, second.Reason)

	for _, phase := range []string{PhaseCommitted, PhaseAborted} {
		notice, err := svc.ClassifyTransaction(ctx, TransactionRequest{
			OwnerName: "user2", RepoName: "repo1", Phase: phase,
			Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/master"}},
			Proof: secret,
		})
		require.NoError(t, err)
		require.True(t, notice.Allowed, phase)
	}
}

func TestClassifyOrdinaryOwnerScope(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := admissionService(t, nil, time.Now().Unix())
	encoded, err := encodeScope(Scope{Kind: model.OwnerOrdinary, RepositoryID: 1, Ref: "refs/heads/topic", Family: "branch"})
	require.NoError(t, err)
	_, err = model.ClaimOrdinary(ctx, "ord:branch/1/topic/z", encoded, execcontext.Verifier("branch-proof"))
	require.NoError(t, err)

	allowed, err := svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: "0000000000000000000000000000000000000000", New: testHead, Ref: "refs/heads/topic"}},
		Proof: "branch-proof",
	})
	require.NoError(t, err)
	require.True(t, allowed.Allowed)

	denied, err := svc.ClassifyTransaction(ctx, TransactionRequest{
		OwnerName: "user2", RepoName: "repo1", Phase: PhasePrepared,
		Lines: []RefLine{{Old: testBase, New: testHead, Ref: "refs/heads/other"}},
		Proof: "branch-proof",
	})
	require.NoError(t, err)
	require.False(t, denied.Allowed)
}

func TestClassifyCompletion(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := admissionService(t, nil, time.Now().Unix())

	idle, err := svc.ClassifyCompletion(ctx, 1, []string{"refs/heads/master"}, "", false)
	require.NoError(t, err)
	require.True(t, idle.Allowed)

	encoded, err := encodeScope(mergeScope())
	require.NoError(t, err)
	owner, err := func() (string, error) {
		owner := conditionalOwner(operationTestInstallation, "op-completion")
		_, err := model.ClaimOrdinary(ctx, owner, encoded, execcontext.Verifier("completion-proof"))
		return owner, err
	}()
	require.NoError(t, err)

	proofLess, err := svc.ClassifyCompletion(ctx, 1, []string{"refs/heads/master"}, "", false)
	require.NoError(t, err)
	require.False(t, proofLess.Allowed)

	withOptions, err := svc.ClassifyCompletion(ctx, 1, []string{"refs/heads/master"}, "completion-proof", true)
	require.NoError(t, err)
	require.False(t, withOptions.Allowed)

	bound, err := svc.ClassifyCompletion(ctx, 1, []string{"refs/heads/master"}, "completion-proof", false)
	require.NoError(t, err)
	require.True(t, bound.Allowed)
	require.NoError(t, model.ReleaseOwner(ctx, owner))
}
