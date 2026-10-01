// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	model "forgejo.org/models/nativeoperation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"
	execcontext "forgejo.org/modules/nativeoperation"

	"github.com/stretchr/testify/require"
)

const (
	publishTestNew        = "1111111111111111111111111111111111111111"
	publishTestOld        = "2222222222222222222222222222222222222222"
	publishTestComparison = "3333333333333333333333333333333333333333"
	publishTestZero       = "0000000000000000000000000000000000000000"
)

func publishIntentPayload(t *testing.T, mutate func(*map[string]any)) []byte {
	t.Helper()
	payload := map[string]any{
		"ref":                     "refs/heads/candidate",
		"expected_old":            "absent",
		"new_oid":                 publishTestNew,
		"comparison_ref":          "refs/heads/master",
		"expected_comparison_oid": publishTestComparison,
	}
	if mutate != nil {
		mutate(&payload)
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return raw
}

func publishIntent(t *testing.T, id string, revision int64, payload []byte) *ValidIntent {
	t.Helper()
	now := time.Now().Unix()
	intent, err := ValidateIntent(id, 2, 1, model.KindRefPublish, "rev-1", revision, now+300, payload, now)
	require.NoError(t, err)
	return intent
}

func TestValidatePublishIntent(t *testing.T) {
	now := time.Now().Unix()
	validate := func(payload []byte) (*ValidIntent, error) {
		return ValidateIntent("op-pub", 2, 1, model.KindRefPublish, "rev-1", 1, now+300, payload, now)
	}

	creation, err := validate(publishIntentPayload(t, nil))
	require.NoError(t, err)
	require.True(t, creation.Publish.IsCreation())
	require.Equal(t, "refs/heads/candidate", creation.Publish.Ref)

	updatePayload := publishIntentPayload(t, func(payload *map[string]any) {
		(*payload)["expected_old"] = publishTestOld
	})
	update, err := validate(updatePayload)
	require.NoError(t, err)
	require.False(t, update.Publish.IsCreation())

	correctionPayload := publishIntentPayload(t, func(payload *map[string]any) {
		(*payload)["expected_old"] = publishTestOld
		(*payload)["pull_request"] = map[string]any{"number": 3, "expected_author_id": 1}
	})
	correction, err := validate(correctionPayload)
	require.NoError(t, err)
	require.NotNil(t, correction.Publish.Correction)

	invalid := map[string][]byte{
		"same ref and comparison": publishIntentPayload(t, func(payload *map[string]any) {
			(*payload)["ref"] = "refs/heads/master"
		}),
		"no-op tuple": publishIntentPayload(t, func(payload *map[string]any) {
			(*payload)["expected_old"] = publishTestNew
		}),
		"creation with PR": publishIntentPayload(t, func(payload *map[string]any) {
			(*payload)["pull_request"] = map[string]any{"number": 3, "expected_author_id": 1}
		}),
		"short OID": publishIntentPayload(t, func(payload *map[string]any) {
			(*payload)["new_oid"] = "short"
		}),
		"empty": nil,
	}
	for name, payload := range invalid {
		if _, err := validate(payload); err == nil {
			t.Fatalf("%s: expected invalid intent", name)
		}
	}
}

// waitingPublishOp inserts a waiting publish operation and returns it with
// the current idle revision bound.
func waitingPublishOp(t *testing.T, svc *Service, id string) *model.Operation {
	t.Helper()
	ctx := t.Context()
	observation, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, observation.Idle)
	intent := publishIntent(t, id, observation.Revision, publishIntentPayload(t, nil))
	op := &model.Operation{
		InstallationID:         operationTestInstallation,
		OperationID:            intent.OperationID,
		Kind:                   intent.Kind,
		ActorID:                2,
		RepositoryID:           1,
		TokenID:                9,
		CredentialFingerprint:  "fingerprint",
		AuthRevision:           intent.AuthRevision,
		ExpectedNativeRevision: intent.ExpectedNativeRevision,
		NotAfter:               intent.NotAfter,
		IntentDigest:           intent.Digest,
		Intent:                 intent.Canonical,
	}
	recorded, err := model.InsertWaitingOperation(ctx, op)
	require.NoError(t, err)
	return recorded
}

func claimPublishOp(t *testing.T, id string, mutate func(*Scope)) (owner, secret string) {
	t.Helper()
	ctx := t.Context()
	secret = "publish-proof-secret-" + id
	scope := Scope{
		Kind:         model.OwnerConditional,
		RepositoryID: 1,
		Ref:          "refs/heads/candidate",
		OldOID:       publishTestZero,
		NewOID:       publishTestNew,
		HeadRef:      "refs/heads/master",
		HeadOID:      publishTestComparison,
	}
	if mutate != nil {
		mutate(&scope)
	}
	encoded, err := encodeScope(scope)
	require.NoError(t, err)
	owner = conditionalOwner(operationTestInstallation, id)
	_, _, err = model.ClaimPublishReceive(ctx, operationTestInstallation, id, owner, encoded, execcontext.Verifier(secret), time.Now().Unix())
	require.NoError(t, err)
	return owner, secret
}

func TestSubmitPublishRegistersWaiting(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	before, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, before.Idle)

	intent := publishIntent(t, "op-pub-submit", before.Revision, publishIntentPayload(t, nil))
	record, err := svc.Submit(ctx, testDecision(), operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, model.EffectPending, record.Outcome)
	require.Equal(t, model.KindRefPublish, record.Kind)

	// Registration occupies no reservation.
	after, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	require.True(t, after.Idle)
	require.Equal(t, before.Revision, after.Revision)

	// Identical replay returns the registration without a second execution.
	replay, err := svc.Submit(ctx, testDecision(), operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, model.EffectPending, replay.Outcome)

	changed := publishIntent(t, "op-pub-submit", before.Revision, publishIntentPayload(t, nil))
	changed.Digest = "different-digest"
	_, err = svc.Submit(ctx, testDecision(), operationTestInstallation, changed)
	require.ErrorIs(t, err, ErrIntentConflict)
}

func TestSubmitPublishBlockedByEarlyCancel(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	svc := NewService()

	observation, err := svc.ReadNativeRevision(ctx)
	require.NoError(t, err)
	cancelled, err := svc.Cancel(ctx, operationTestInstallation, "op-pub-cancelled")
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, cancelled.Outcome)

	_, err = svc.Submit(ctx, testDecision(), operationTestInstallation,
		publishIntent(t, "op-pub-cancelled", observation.Revision, publishIntentPayload(t, nil)))
	require.ErrorIs(t, err, ErrCancelledBeforeSubmit)
}

func TestPublishRefusalReason(t *testing.T) {
	require.Equal(t, model.ReasonStaleHead, publishRefusalReason(ExactPublishRefusedBranchExists))
	require.Equal(t, model.ReasonStaleHead, publishRefusalReason(ExactPublishRefusedNotFastForward))
	require.Equal(t, model.ReasonStaleBaseOrResult, publishRefusalReason(ExactPublishRefusedStaleCompare))
	require.Equal(t, model.ReasonPRMismatch, publishRefusalReason(ExactPublishRefusedClosedPR))
	require.Equal(t, model.ReasonNativeRefused, publishRefusalReason(ExactPublishRefusedMalformedOID))
	require.Equal(t, model.ReasonNativeRefused, publishRefusalReason("future reason"))
}

func TestClassifyPreReceivePublish(t *testing.T) {
	tuple := []RefLine{{Old: publishTestZero, New: publishTestNew, Ref: "refs/heads/candidate"}}

	t.Run("IdleAllows", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		svc := NewService()
		decision, err := svc.ClassifyPreReceive(t.Context(), 1, tuple, false, PublishPreReceiveGit{})
		require.NoError(t, err)
		require.True(t, decision.Allowed)
	})

	t.Run("OrdinaryOwnerAllows", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := NewService()
		_, err := model.ClaimOrdinary(ctx, "ord:receive-http/1/main/nonce", `{"family":"receive-http"}`, "v")
		require.NoError(t, err)
		decision, err := svc.ClassifyPreReceive(ctx, 1, tuple, false, PublishPreReceiveGit{})
		require.NoError(t, err)
		require.True(t, decision.Allowed)
	})

	t.Run("MergeOwnerAllows", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := NewService()
		now := time.Now().Unix()
		intent, err := ValidateIntent("op-merge-owner", 2, 1, model.KindMerge, "rev-1", 1, now+300, mergePayload(t, nil), now)
		require.NoError(t, err)
		op := &model.Operation{
			InstallationID: operationTestInstallation, OperationID: intent.OperationID,
			Kind: intent.Kind, ActorID: 2, RepositoryID: 1, Submitted: true,
			EffectState: model.EffectPending, ExpectedNativeRevision: 1, NotAfter: now + 300,
			IntentDigest: intent.Digest, Intent: intent.Canonical,
		}
		_, err = model.ClaimConditional(ctx, op, conditionalOwner(operationTestInstallation, intent.OperationID), `{}`, "v")
		require.NoError(t, err)
		decision, err := svc.ClassifyPreReceive(ctx, 1, tuple, false, PublishPreReceiveGit{})
		require.NoError(t, err)
		require.True(t, decision.Allowed)
	})

	t.Run("ExactTupleAllows", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := NewService()
		waitingPublishOp(t, svc, "op-pre-exact")
		claimPublishOp(t, "op-pre-exact", nil)
		decision, err := svc.ClassifyPreReceive(ctx, 1, tuple, false, PublishPreReceiveGit{})
		require.NoError(t, err)
		require.True(t, decision.Allowed)
	})

	t.Run("ExtraCommandRefusesAndRecords", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := NewService()
		waitingPublishOp(t, svc, "op-pre-extra")
		claimPublishOp(t, "op-pre-extra", nil)
		decision, err := svc.ClassifyPreReceive(ctx, 1, append(tuple, RefLine{Old: publishTestZero, New: publishTestNew, Ref: "refs/heads/other"}), false, PublishPreReceiveGit{})
		require.NoError(t, err)
		require.False(t, decision.Allowed)
		require.Equal(t, model.ReasonUnexpectedRefEffects, decision.Reason)
		op, err := model.LookupOperation(ctx, operationTestInstallation, "op-pre-extra")
		require.NoError(t, err)
		require.Equal(t, model.ReasonUnexpectedRefEffects, op.Reason)
		require.False(t, op.Admitted)
	})

	t.Run("TupleMismatchRefuses", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := NewService()
		waitingPublishOp(t, svc, "op-pre-tuple")
		claimPublishOp(t, "op-pre-tuple", nil)
		decision, err := svc.ClassifyPreReceive(ctx, 1, []RefLine{{Old: publishTestZero, New: publishTestOld, Ref: "refs/heads/candidate"}}, false, PublishPreReceiveGit{})
		require.NoError(t, err)
		require.False(t, decision.Allowed)
		require.Equal(t, model.ReasonStaleBaseOrResult, decision.Reason)
	})

	t.Run("PushOptionsRefuse", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := NewService()
		waitingPublishOp(t, svc, "op-pre-options")
		claimPublishOp(t, "op-pre-options", nil)
		decision, err := svc.ClassifyPreReceive(ctx, 1, tuple, true, PublishPreReceiveGit{})
		require.NoError(t, err)
		require.False(t, decision.Allowed)
		require.Equal(t, model.ReasonPublishOptionsRejected, decision.Reason)
	})

	t.Run("WrongRepositoryRefuses", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := NewService()
		waitingPublishOp(t, svc, "op-pre-repo")
		claimPublishOp(t, "op-pre-repo", nil)
		decision, err := svc.ClassifyPreReceive(ctx, 2, tuple, false, PublishPreReceiveGit{})
		require.NoError(t, err)
		require.False(t, decision.Allowed)
	})
}

func TestClassifyCompletionKindCheck(t *testing.T) {
	t.Run("PublishOwnerAllowsScopedRef", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := NewService()
		waitingPublishOp(t, svc, "op-complete-pub")
		_, secret := claimPublishOp(t, "op-complete-pub", nil)
		decision, err := svc.ClassifyCompletion(ctx, 1, []string{"refs/heads/candidate"}, secret, false)
		require.NoError(t, err)
		require.True(t, decision.Allowed)
	})

	t.Run("UnknownKindRefuses", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := NewService()
		observation, err := svc.ReadNativeRevision(ctx)
		require.NoError(t, err)
		future := &model.Operation{
			InstallationID: operationTestInstallation, OperationID: "op-complete-future",
			Kind: "future.kind", ActorID: 2, RepositoryID: 1,
			ExpectedNativeRevision: observation.Revision, NotAfter: time.Now().Unix() + 300,
			IntentDigest: "digest", Intent: `{"operation_id":"op-complete-future"}`,
		}
		_, err = model.InsertWaitingOperation(ctx, future)
		require.NoError(t, err)
		owner := conditionalOwner(operationTestInstallation, "op-complete-future")
		secret := "future-proof"
		_, _, err = model.ClaimPublishReceive(ctx, operationTestInstallation, "op-complete-future", owner,
			`{"kind":"conditional","repository_id":1,"ref":"refs/heads/candidate"}`, execcontext.Verifier(secret), time.Now().Unix())
		require.NoError(t, err)
		decision, err := svc.ClassifyCompletion(ctx, 1, []string{"refs/heads/candidate"}, secret, false)
		require.NoError(t, err)
		require.False(t, decision.Allowed)
	})

	t.Run("MergeOwnerStillAllows", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := NewService()
		now := time.Now().Unix()
		intent, err := ValidateIntent("op-complete-merge", 2, 1, model.KindMerge, "rev-1", 1, now+300, mergePayload(t, nil), now)
		require.NoError(t, err)
		op := &model.Operation{
			InstallationID: operationTestInstallation, OperationID: intent.OperationID,
			Kind: intent.Kind, ActorID: 2, RepositoryID: 1, Submitted: true,
			EffectState: model.EffectPending, ExpectedNativeRevision: 1, NotAfter: now + 300,
			IntentDigest: intent.Digest, Intent: intent.Canonical,
		}
		owner := conditionalOwner(operationTestInstallation, intent.OperationID)
		secret := "merge-proof"
		_, err = model.ClaimConditional(ctx, op, owner, `{"kind":"conditional","repository_id":1,"ref":"refs/heads/master"}`, execcontext.Verifier(secret))
		require.NoError(t, err)
		decision, err := svc.ClassifyCompletion(ctx, 1, []string{"refs/heads/master"}, secret, false)
		require.NoError(t, err)
		require.True(t, decision.Allowed)
	})
}

// publishStubRefs serves reconcile tips with explicit absent refs: absent
// refs report git.ErrNotExist like a missing branch, present refs report
// their tip, and failAll simulates an unreadable repository.
type publishStubRefs struct {
	tips    map[string]string
	absent  map[string]bool
	failAll bool
}

func (s publishStubRefs) CommitID(_ context.Context, _ string, ref string) (string, error) {
	if s.failAll {
		return "", errors.New("repository unreadable")
	}
	if s.absent[ref] {
		return "", git.ErrNotExist{}
	}
	tip, ok := s.tips[ref]
	if !ok {
		return "", git.ErrNotExist{}
	}
	return tip, nil
}

func reconcileService(t *testing.T, refs publishStubRefs) *Service {
	t.Helper()
	return &Service{refs: refs, now: func() int64 { return time.Now().Unix() }, capabilityDir: t.TempDir()}
}

func reconcilePrepared(t *testing.T, svc *Service, id string) *PreparedPublishReceive {
	t.Helper()
	waitingPublishOp(t, svc, id)
	owner, _ := claimPublishOp(t, id, nil)
	intent, err := ParsePublishPayload(publishIntentPayload(t, nil))
	require.NoError(t, err)
	repository := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	op, err := model.LookupOperation(t.Context(), operationTestInstallation, id)
	require.NoError(t, err)
	return &PreparedPublishReceive{
		Operation:  op,
		Intent:     intent,
		Target:     &PublishTarget{Ref: "refs/heads/candidate", OldOID: publishTestZero, NewOID: publishTestNew, ComparisonRef: "refs/heads/master", ComparisonOID: publishTestComparison},
		Execution:  &execcontext.Execution{Owner: owner, Generation: 1},
		Repository: repository,
	}
}

func TestReconcilePublish(t *testing.T) {
	committedTips := func() publishStubRefs {
		return publishStubRefs{tips: map[string]string{
			"refs/heads/candidate": publishTestNew,
			"refs/heads/master":    publishTestComparison,
		}}
	}

	t.Run("CommittedComplete", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := reconcileService(t, committedTips())
		prepared := reconcilePrepared(t, svc, "op-rec-commit")
		_, admitted, err := model.RecordAdmissionAttempt(ctx, operationTestInstallation, "op-rec-commit", prepared.Execution.Owner, true, "")
		require.NoError(t, err)
		require.True(t, admitted)
		record, terminal, err := svc.ReconcilePublish(ctx, prepared, nil)
		require.NoError(t, err)
		require.False(t, terminal.Retained())
		require.Equal(t, model.EffectCommitted, record.Outcome)
		require.Equal(t, model.CompletionComplete, record.CompletionState)
		require.Contains(t, string(record.Receipt), publishTestNew)
		require.Contains(t, string(record.Receipt), "refs/heads/candidate")
		idle, err := svc.ReadNativeRevision(ctx)
		require.NoError(t, err)
		require.True(t, idle.Idle)
	})

	t.Run("CommittedNeedsInterventionOnReceiverError", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := reconcileService(t, committedTips())
		prepared := reconcilePrepared(t, svc, "op-rec-failed-rx")
		_, admitted, err := model.RecordAdmissionAttempt(ctx, operationTestInstallation, "op-rec-failed-rx", prepared.Execution.Owner, true, "")
		require.NoError(t, err)
		require.True(t, admitted)
		record, terminal, err := svc.ReconcilePublish(ctx, prepared, errors.New("receive-pack failed"))
		require.NoError(t, err)
		require.False(t, terminal.Retained())
		require.Equal(t, model.EffectCommitted, record.Outcome)
		require.Equal(t, model.CompletionNeedsIntervention, record.CompletionState)
	})

	t.Run("NativelyRefusedCreation", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := reconcileService(t, publishStubRefs{
			tips:   map[string]string{"refs/heads/master": publishTestComparison},
			absent: map[string]bool{"refs/heads/candidate": true},
		})
		prepared := reconcilePrepared(t, svc, "op-rec-refused")
		record, terminal, err := svc.ReconcilePublish(ctx, prepared, errors.New("receive-pack failed"))
		require.NoError(t, err)
		require.False(t, terminal.Retained())
		require.Equal(t, model.EffectNotCommitted, record.Outcome)
		require.Equal(t, model.ReasonNativeRefused, record.ReasonCode)
		idle, err := svc.ReadNativeRevision(ctx)
		require.NoError(t, err)
		require.True(t, idle.Idle)
	})

	t.Run("RefusedKeepsRecordedReason", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := reconcileService(t, publishStubRefs{
			tips:   map[string]string{"refs/heads/master": publishTestComparison},
			absent: map[string]bool{"refs/heads/candidate": true},
		})
		prepared := reconcilePrepared(t, svc, "op-rec-reason")
		_, admitted, err := model.RecordAdmissionAttempt(ctx, operationTestInstallation, "op-rec-reason", prepared.Execution.Owner, false, model.ReasonUnexpectedRefEffects)
		require.NoError(t, err)
		require.False(t, admitted)
		record, terminal, err := svc.ReconcilePublish(ctx, prepared, errors.New("receive-pack failed"))
		require.NoError(t, err)
		require.False(t, terminal.Retained())
		require.Equal(t, model.EffectNotCommitted, record.Outcome)
		require.Equal(t, model.ReasonUnexpectedRefEffects, record.ReasonCode)
	})

	t.Run("AdmittedTipMismatchStaysIndeterminate", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := reconcileService(t, publishStubRefs{tips: map[string]string{
			"refs/heads/candidate": publishTestOld,
			"refs/heads/master":    publishTestComparison,
		}})
		prepared := reconcilePrepared(t, svc, "op-rec-mismatch")
		_, admitted, err := model.RecordAdmissionAttempt(ctx, operationTestInstallation, "op-rec-mismatch", prepared.Execution.Owner, true, "")
		require.NoError(t, err)
		require.True(t, admitted)
		record, terminal, err := svc.ReconcilePublish(ctx, prepared, nil)
		require.NoError(t, err)
		require.True(t, terminal.Retained())
		require.Equal(t, model.EffectIndeterminate, record.Outcome)
		busy, err := svc.ReadNativeRevision(ctx)
		require.NoError(t, err)
		require.False(t, busy.Idle)
	})

	t.Run("UnreadableRepositoryStaysIndeterminate", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		svc := reconcileService(t, publishStubRefs{failAll: true})
		prepared := reconcilePrepared(t, svc, "op-rec-unreadable")
		record, terminal, err := svc.ReconcilePublish(ctx, prepared, nil)
		require.NoError(t, err)
		require.True(t, terminal.Retained())
		require.Equal(t, model.EffectIndeterminate, record.Outcome)
	})
}

func TestRecoverConditionalPublish(t *testing.T) {
	committedTips := map[string]string{
		"refs/heads/candidate": publishTestNew,
		"refs/heads/master":    publishTestComparison,
	}

	t.Run("CommittedReleases", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		useIsolatedAppData(t)
		svc := admissionService(t, committedTips, time.Now().Unix())
		waitingPublishOp(t, svc, "op-recov-commit")
		owner, _ := claimPublishOp(t, "op-recov-commit", nil)
		claimed, err := model.ReadReservation(ctx)
		require.NoError(t, err)
		_, admitted, err := model.RecordAdmissionAttempt(ctx, operationTestInstallation, "op-recov-commit", owner, true, "")
		require.NoError(t, err)
		require.True(t, admitted)
		inhibitDomain(t)
		assessment, err := svc.Recover(ctx, owner, claimed.Generation)
		require.NoError(t, err)
		require.Equal(t, RecoveryReleased, assessment.Verdict)
		require.Equal(t, model.EffectCommitted, assessment.Effect)
		require.Equal(t, "conditional-publish", assessment.Family)
		op, err := model.LookupOperation(ctx, operationTestInstallation, "op-recov-commit")
		require.NoError(t, err)
		require.Contains(t, op.Receipt, publishTestNew)
	})

	t.Run("UnadmittedOldTipReleases", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		useIsolatedAppData(t)
		// An update scope whose tip still equals the old OID reconciles
		// as not_committed.
		svc := admissionService(t, map[string]string{
			"refs/heads/candidate": publishTestOld,
			"refs/heads/master":    publishTestComparison,
		}, time.Now().Unix())
		waitingPublishOp(t, svc, "op-recov-noeffect")
		owner, _ := claimPublishOp(t, "op-recov-noeffect", func(scope *Scope) {
			scope.OldOID = publishTestOld
			scope.NewOID = publishTestNew
		})
		claimed, err := model.ReadReservation(ctx)
		require.NoError(t, err)
		inhibitDomain(t)
		assessment, err := svc.Recover(ctx, owner, claimed.Generation)
		require.NoError(t, err)
		require.Equal(t, RecoveryReleased, assessment.Verdict)
		require.Equal(t, model.EffectNotCommitted, assessment.Effect)
		op, err := model.LookupOperation(ctx, operationTestInstallation, "op-recov-noeffect")
		require.NoError(t, err)
		require.Equal(t, model.ReasonRecoveredNoEffect, op.Reason)
	})

	t.Run("CorrectionBindsPRReceipt", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		useIsolatedAppData(t)
		// Fixture PR 3 on repo 1 is open with head branch2, base master
		// and poster 1; a matching correction scope attributes its IDs.
		svc := admissionService(t, map[string]string{
			"refs/heads/branch2": publishTestNew,
			"refs/heads/master":  publishTestComparison,
		}, time.Now().Unix())
		waitingPublishOp(t, svc, "op-recov-correction")
		owner, _ := claimPublishOp(t, "op-recov-correction", func(scope *Scope) {
			scope.Ref = "refs/heads/branch2"
			scope.OldOID = publishTestOld
			scope.NewOID = publishTestNew
			scope.PRNumber = 3
			scope.CorrectionAuthorID = 1
		})
		claimed, err := model.ReadReservation(ctx)
		require.NoError(t, err)
		_, admitted, err := model.RecordAdmissionAttempt(ctx, operationTestInstallation, "op-recov-correction", owner, true, "")
		require.NoError(t, err)
		require.True(t, admitted)
		inhibitDomain(t)
		assessment, err := svc.Recover(ctx, owner, claimed.Generation)
		require.NoError(t, err)
		require.Equal(t, RecoveryReleased, assessment.Verdict)
		require.Equal(t, model.EffectCommitted, assessment.Effect)
		op, err := model.LookupOperation(ctx, operationTestInstallation, "op-recov-correction")
		require.NoError(t, err)
		require.Contains(t, op.Receipt, `"pr_id":2`)
		require.Contains(t, op.Receipt, `"issue_id":3`)
	})

	t.Run("MismatchStaysFenced", func(t *testing.T) {
		unittest.PrepareTestEnv(t)
		ctx := t.Context()
		useIsolatedAppData(t)
		svc := admissionService(t, map[string]string{
			"refs/heads/candidate": publishTestOld,
			"refs/heads/master":    publishTestComparison,
		}, time.Now().Unix())
		waitingPublishOp(t, svc, "op-recov-fence")
		owner, _ := claimPublishOp(t, "op-recov-fence", nil)
		claimed, err := model.ReadReservation(ctx)
		require.NoError(t, err)
		_, admitted, err := model.RecordAdmissionAttempt(ctx, operationTestInstallation, "op-recov-fence", owner, true, "")
		require.NoError(t, err)
		require.True(t, admitted)
		inhibitDomain(t)
		assessment, err := svc.Recover(ctx, owner, claimed.Generation)
		require.NoError(t, err)
		require.Equal(t, RecoveryFenced, assessment.Verdict)
	})
}
