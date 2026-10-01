// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	sdk "forgejo.org/extension-sdk"
	authmodel "forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	execcontext "forgejo.org/modules/nativeoperation"
	pull_service "forgejo.org/services/pull"
)

// submitMerge claims the operation ID and reservation, executes one native
// fast-forward-only merge under the held owner, and reconciles the
// attributable result. There is no unguarded fallback: every refusal path
// records not_committed or retains the fence.
func (s *Service) submitMerge(ctx context.Context, decision authmodel.SubmissionDecision, installationID string, intent *ValidIntent) (sdk.OperationRecord, error) {
	if err := ctx.Err(); err != nil {
		return sdk.OperationRecord{}, err
	}
	dir, err := s.execDir()
	if err != nil {
		return sdk.OperationRecord{}, err
	}
	capabilityPath, secret, err := execcontext.WriteCapabilityFile(dir)
	if err != nil {
		return sdk.OperationRecord{}, err
	}
	retire := func() { _ = os.Remove(capabilityPath) }
	owner := conditionalOwner(installationID, intent.OperationID)
	scope := Scope{
		Kind:         model.OwnerConditional,
		RepositoryID: intent.RepositoryID,
		Ref:          intent.Merge.BaseRef,
		OldOID:       intent.Merge.ExpectedBaseOID,
		NewOID:       intent.Merge.ExpectedHeadOID,
		HeadRef:      intent.Merge.HeadRef,
		HeadOID:      intent.Merge.ExpectedHeadOID,
		PRNumber:     intent.Merge.PullRequestNumber,
	}
	encoded, err := encodeScope(scope)
	if err != nil {
		retire()
		return sdk.OperationRecord{}, err
	}
	op := &model.Operation{
		InstallationID:         installationID,
		OperationID:            intent.OperationID,
		Kind:                   intent.Kind,
		ActorID:                decision.ActorID,
		RepositoryID:           intent.RepositoryID,
		TokenID:                decision.TokenID,
		CredentialFingerprint:  decision.CredentialFingerprint,
		AuthRevision:           intent.AuthRevision,
		ExpectedNativeRevision: intent.ExpectedNativeRevision,
		NotAfter:               intent.NotAfter,
		IntentDigest:           intent.Digest,
		Intent:                 intent.Canonical,
	}
	claimed, err := model.ClaimConditional(ctx, op, owner, encoded, execcontext.Verifier(secret))
	if err != nil {
		retire()
		if errors.Is(err, model.ErrBusy) {
			return sdk.OperationRecord{}, ErrBusy
		}
		if errors.Is(err, model.ErrInhibited) {
			return sdk.OperationRecord{}, err
		}
		if errors.Is(err, model.ErrStaleRevision) {
			return s.recordStaleRevision(ctx, op)
		}
		// A concurrent identical submit may have won the insert; reread
		// and reconcile rather than launching a second execution.
		existing, lookupErr := model.LookupOperation(ctx, installationID, intent.OperationID)
		if lookupErr != nil {
			return sdk.OperationRecord{}, lookupErr
		}
		if existing != nil {
			return s.reconcileExisting(existing, intent)
		}
		return sdk.OperationRecord{}, err
	}
	execution := &execcontext.Execution{Owner: owner, Generation: claimed.Generation, CapabilityPath: capabilityPath}
	record, terminal, err := s.executeMerge(ctx, op, intent, scope, execution)
	if err != nil {
		// Infrastructure failure before reconciliation: fail closed with
		// the owner held and the capability kept for later recovery.
		return sdk.OperationRecord{}, err
	}
	if !terminal.retain {
		// SetTerminal already released the owner; retire the file.
		retire()
	}
	return record, nil
}

func (s *Service) recordStaleRevision(ctx context.Context, op *model.Operation) (sdk.OperationRecord, error) {
	op.EffectState = model.EffectNotCommitted
	op.Reason = model.ReasonStaleNativeRevision
	op.Cancellation = model.CancellationNone
	recorded, err := model.InsertRefusedOperation(ctx, op)
	if err != nil && !errors.Is(err, model.ErrDuplicateOperation) {
		return sdk.OperationRecord{}, err
	}
	return ToRecord(recorded), nil
}

type terminalResult struct {
	retain bool
}

// Retained reports whether the owner stays held for an unresolved effect.
func (t terminalResult) Retained() bool {
	return t.retain
}

// executeMerge runs the guarded merge and reconciles its result. It returns
// the SDK record and whether the owner stays held for an unresolved effect.
func (s *Service) executeMerge(ctx context.Context, op *model.Operation, intent *ValidIntent, scope Scope, execution *execcontext.Execution) (sdk.OperationRecord, terminalResult, error) {
	owner := execution.Owner
	// Terminal records and the owner release must complete even when the
	// caller disconnects mid-write; native work above keeps the live
	// context so cancellation still stops it.
	releaseCtx := context.WithoutCancel(ctx)
	refuse := func(reason string) (sdk.OperationRecord, terminalResult, error) {
		recorded, err := model.SetTerminal(releaseCtx, op.InstallationID, op.OperationID, model.TerminalOutcome{
			EffectState:  model.EffectNotCommitted,
			Reason:       reason,
			Cancellation: model.CancellationNone,
		}, owner)
		if err != nil {
			return sdk.OperationRecord{}, terminalResult{}, err
		}
		return ToRecord(recorded), terminalResult{}, nil
	}
	if err := s.revalidateSubmission(ctx, op); err != nil {
		if errors.Is(err, ErrAuthorityLost) {
			return refuse(model.ReasonAuthorityLost)
		}
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	repository, err := repo_model.GetRepositoryByID(ctx, intent.RepositoryID)
	if err != nil {
		return refuse(model.ReasonNativeRefused)
	}
	pr, err := issues_model.GetPullRequestByIndex(ctx, intent.RepositoryID, intent.Merge.PullRequestNumber)
	if err != nil {
		return refuse(model.ReasonPRMismatch)
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return refuse(model.ReasonNativeRefused)
	}
	if !sameRepositoryPullRequest(pr, intent) {
		return refuse(model.ReasonPRMismatch)
	}
	if s.clock() >= op.NotAfter {
		return refuse(model.ReasonExpiredBeforeAdmission)
	}
	head, err := s.readRef(ctx, repository.RepoPath(), intent.Merge.HeadRef)
	if err != nil || !strings.EqualFold(head, intent.Merge.ExpectedHeadOID) {
		return refuse(model.ReasonStaleHead)
	}
	base, err := s.readRef(ctx, repository.RepoPath(), intent.Merge.BaseRef)
	if err != nil || !strings.EqualFold(base, intent.Merge.ExpectedBaseOID) {
		return refuse(model.ReasonStaleBaseOrResult)
	}
	doer, err := user_model.GetUserByID(ctx, op.ActorID)
	if err != nil {
		return refuse(model.ReasonAuthorityLost)
	}
	permission, err := access_model.GetUserRepoPermission(ctx, repository, doer)
	if err != nil {
		return refuse(model.ReasonAuthorityLost)
	}
	// Retain the native mergeability layer: permission, protection, review,
	// status and merge-method enforcement stay effective under the gate.
	if err := pull_service.CheckPullMergeable(ctx, doer, &permission, pr, pull_service.MergeCheckTypeGeneral, false); err != nil {
		return refuse(model.ReasonNativeRefused)
	}
	baseGitRepo, err := gitrepo.OpenRepository(ctx, repository)
	if err != nil {
		return refuse(model.ReasonNativeRefused)
	}
	defer baseGitRepo.Close()
	message, _, err := pull_service.GetDefaultMergeMessage(ctx, baseGitRepo, pr, repo_model.MergeStyleFastForwardOnly)
	if err != nil {
		return refuse(model.ReasonNativeRefused)
	}
	owned := execcontext.NewContext(ctx, execution)
	mergeErr := pull_service.Merge(owned, pr, doer, baseGitRepo, repo_model.MergeStyleFastForwardOnly, intent.Merge.ExpectedHeadOID, message, false)
	// Test-only crash barrier for offline-recovery proof: with the native
	// merge done and the owner still held, the driver SIGKILLs the server
	// here to simulate a crash before reconciliation.
	if err := TestCrashBarrier(CrashPointMergeAfterNative); err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	return s.reconcileMerge(releaseCtx, op, scope, repository, execution, mergeErr)
}

func sameRepositoryPullRequest(pr *issues_model.PullRequest, intent *ValidIntent) bool {
	if pr == nil || pr.HasMerged || pr.HeadRepoID != intent.Merge.HeadRepositoryID {
		return false
	}
	if pr.HeadBranch != strings.TrimPrefix(intent.Merge.HeadRef, git.BranchPrefix) {
		return false
	}
	if pr.BaseBranch != strings.TrimPrefix(intent.Merge.BaseRef, git.BranchPrefix) {
		return false
	}
	if pr.BaseRepoID != intent.RepositoryID {
		return false
	}
	return pr.Issue != nil && !pr.Issue.IsClosed
}

// MergeReceipt attributes one committed merge to its operation.
type MergeReceipt struct {
	OldOID       string `json:"old_oid"`
	NewOID       string `json:"new_oid"`
	ActorID      int64  `json:"actor_id"`
	RepositoryID int64  `json:"repository_id"`
	PRNumber     int64  `json:"pr_number"`
	PRID         int64  `json:"pr_id"`
	HeadRef      string `json:"head_ref"`
	BaseRef      string `json:"base_ref"`
	Method       string `json:"method"`
}

// reconcileMerge resolves the write after writer quiescence. The merge
// engine's error alone never decides: attribution comes from the recorded
// admission plus the authoritative ref tip. A known effect releases the
// owner; an unknown effect retains the fence.
func (s *Service) reconcileMerge(ctx context.Context, op *model.Operation, scope Scope, repository *repo_model.Repository, execution *execcontext.Execution, mergeErr error) (sdk.OperationRecord, terminalResult, error) {
	owner := execution.Owner
	fresh, err := model.LookupOperation(ctx, op.InstallationID, op.OperationID)
	if err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	if fresh == nil {
		return sdk.OperationRecord{}, terminalResult{}, errors.New("operation vanished during execution")
	}
	_ = mergeErr
	tip, tipErr := s.readRef(ctx, repository.RepoPath(), scope.Ref)
	if tipErr != nil {
		recorded, err := model.SetTerminal(ctx, op.InstallationID, op.OperationID, model.TerminalOutcome{
			EffectState:  model.EffectIndeterminate,
			Cancellation: model.CancellationNone,
		}, "")
		if err != nil {
			return sdk.OperationRecord{}, terminalResult{}, err
		}
		return ToRecord(recorded), terminalResult{retain: true}, nil
	}
	tip = strings.ToLower(tip)
	committed := fresh.Admitted && tip == scope.NewOID && tip != scope.OldOID
	if committed {
		receipt, _ := json.Marshal(MergeReceipt{
			OldOID:       scope.OldOID,
			NewOID:       scope.NewOID,
			ActorID:      op.ActorID,
			RepositoryID: op.RepositoryID,
			PRNumber:     scope.PRNumber,
			PRID:         mergedPRID(ctx, repository.ID, scope.PRNumber),
			HeadRef:      scope.HeadRef,
			BaseRef:      scope.Ref,
			Method:       "fast-forward-only",
		})
		completion := model.CompletionNeedsIntervention
		if prMergedAt(ctx, repository.ID, scope.PRNumber, tip) {
			completion = model.CompletionComplete
		}
		recorded, err := model.SetTerminal(ctx, op.InstallationID, op.OperationID, model.TerminalOutcome{
			EffectState:  model.EffectCommitted,
			Cancellation: model.CancellationNone,
			Completion:   completion,
			Receipt:      string(receipt),
		}, owner)
		if err != nil {
			return sdk.OperationRecord{}, terminalResult{}, err
		}
		return ToRecord(recorded), terminalResult{}, nil
	}
	if fresh.Admitted || (tip == scope.NewOID && tip != scope.OldOID) {
		// Admitted but tip disagrees, or the tip moved without admission:
		// the effect cannot be attributed, so it stays fenced.
		recorded, err := model.SetTerminal(ctx, op.InstallationID, op.OperationID, model.TerminalOutcome{
			EffectState:  model.EffectIndeterminate,
			Cancellation: model.CancellationNone,
		}, "")
		if err != nil {
			return sdk.OperationRecord{}, terminalResult{}, err
		}
		return ToRecord(recorded), terminalResult{retain: true}, nil
	}
	reason := fresh.Reason
	if reason == "" {
		reason = model.ReasonNativeRefused
	}
	recorded, err := model.SetTerminal(ctx, op.InstallationID, op.OperationID, model.TerminalOutcome{
		EffectState:  model.EffectNotCommitted,
		Reason:       reason,
		Cancellation: model.CancellationNone,
	}, owner)
	if err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	return ToRecord(recorded), terminalResult{}, nil
}

func mergedPRID(ctx context.Context, repoID, number int64) int64 {
	pr, err := issues_model.GetPullRequestByIndex(ctx, repoID, number)
	if err != nil || pr == nil {
		return 0
	}
	return pr.ID
}

func prMergedAt(ctx context.Context, repoID, number int64, tip string) bool {
	pr, err := issues_model.GetPullRequestByIndex(ctx, repoID, number)
	if err != nil || pr == nil {
		return false
	}
	return pr.HasMerged && strings.EqualFold(pr.MergedCommitID, tip)
}
