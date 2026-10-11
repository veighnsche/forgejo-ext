// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	sdk "forgejo.org/extension-sdk"
	authmodel "forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	perm_model "forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	execcontext "forgejo.org/modules/nativeoperation"
	notify_service "forgejo.org/services/notify"
)

// LifecycleIssueIntent carries the validated issue.lifecycle.apply
// payload: at most one configured label and one eligible assignee on one
// exact generated issue. Label creation/removal and multi-assignee writes
// are outside this operation.
type LifecycleIssueIntent struct {
	IssueID    int64 `json:"issue_id"`
	LabelID    int64 `json:"label_id,omitempty"`
	AssigneeID int64 `json:"assignee_id,omitempty"`
}

// parseLifecycleIssuePayload validates the complete immutable
// label/assignee intent. Unknown fields and empty effects fail validation
// rather than silently broadening the request.
func parseLifecycleIssuePayload(payload []byte, repositoryID int64) (*LifecycleIssueIntent, error) {
	if len(payload) == 0 || repositoryID <= 0 {
		return nil, ErrInvalidIntent
	}
	var intent LifecycleIssueIntent
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return nil, ErrInvalidIntent
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidIntent
	}
	if intent.IssueID <= 0 || intent.LabelID < 0 || intent.AssigneeID < 0 {
		return nil, ErrInvalidIntent
	}
	if intent.LabelID == 0 && intent.AssigneeID == 0 {
		return nil, ErrInvalidIntent
	}
	return &intent, nil
}

// LifecycleIssueReceipt attributes one committed label/assignee write to
// its operation. Added flags distinguish a new write from an idempotent
// no-op over already-present state.
type LifecycleIssueReceipt struct {
	IssueID       int64 `json:"issue_id"`
	LabelID       int64 `json:"label_id,omitempty"`
	LabelAdded    bool  `json:"label_added,omitempty"`
	AssigneeID    int64 `json:"assignee_id,omitempty"`
	AssigneeAdded bool  `json:"assignee_added,omitempty"`
}

// submitLifecycleIssue claims the operation ID and reservation, verifies
// the exact issue/label/assignee and native authority under the held
// owner, and commits the label/assignee rows with the operation receipt
// in one SQL transaction. Every refusal path records not_committed.
func (s *Service) submitLifecycleIssue(ctx context.Context, decision authmodel.SubmissionDecision, installationID string, intent *ValidIntent) (sdk.OperationRecord, error) {
	if err := ctx.Err(); err != nil {
		return sdk.OperationRecord{}, err
	}
	if intent.LifecycleIssue == nil {
		return sdk.OperationRecord{}, ErrInvalidIntent
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
		Kind:                model.OwnerConditional,
		RepositoryID:        intent.RepositoryID,
		IssueID:             intent.LifecycleIssue.IssueID,
		LifecycleLabelID:    intent.LifecycleIssue.LabelID,
		LifecycleAssigneeID: intent.LifecycleIssue.AssigneeID,
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
	record, terminal, err := s.executeLifecycleIssue(ctx, op, intent, execution)
	if err != nil {
		return sdk.OperationRecord{}, err
	}
	if !terminal.retain {
		retire()
	}
	return record, nil
}

// executeLifecycleIssue runs the guarded label/assignee write. All native
// work runs on the owned context; terminal records and the owner release
// run detached so a caller disconnect cannot stick the reservation.
func (s *Service) executeLifecycleIssue(ctx context.Context, op *model.Operation, intent *ValidIntent, execution *execcontext.Execution) (sdk.OperationRecord, terminalResult, error) {
	owner := execution.Owner
	submit := intent.LifecycleIssue
	releaseCtx := context.WithoutCancel(ctx)
	owned := execcontext.NewContext(ctx, execution)
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
	if err := s.revalidateSubmission(owned, op); err != nil {
		if errors.Is(err, ErrAuthorityLost) {
			return refuse(model.ReasonAuthorityLost)
		}
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	repository, err := repo_model.GetRepositoryByID(owned, op.RepositoryID)
	if err != nil {
		return refuse(model.ReasonNativeRefused)
	}
	if s.clock() >= op.NotAfter {
		return refuse(model.ReasonExpiredBeforeAdmission)
	}
	doer, err := user_model.GetUserByID(owned, op.ActorID)
	if err != nil {
		return refuse(model.ReasonAuthorityLost)
	}
	issue, err := issues_model.GetIssueByID(owned, submit.IssueID)
	if err != nil || issue.IsPull {
		return refuse(model.ReasonNativeRefused)
	}
	if issue.RepoID != op.RepositoryID {
		return refuse(model.ReasonNativeRefused)
	}
	writable, err := access_model.HasAccessUnit(owned, doer, repository, unit.TypeIssues, perm_model.AccessModeWrite)
	if err != nil || !writable {
		return refuse(model.ReasonAuthorityLost)
	}
	var label *issues_model.Label
	if submit.LabelID > 0 {
		label, err = issues_model.GetLabelByID(owned, submit.LabelID)
		if err != nil {
			return refuse(model.ReasonNativeRefused)
		}
		if label.RepoID != op.RepositoryID && (label.OrgID == 0 || label.OrgID != repository.OwnerID) {
			return refuse(model.ReasonNativeRefused)
		}
	}
	var assignee *user_model.User
	if submit.AssigneeID > 0 {
		assignee, err = user_model.GetUserByID(owned, submit.AssigneeID)
		if err != nil {
			return refuse(model.ReasonNativeRefused)
		}
		if err := issue.LoadRepo(owned); err != nil {
			return sdk.OperationRecord{}, terminalResult{}, err
		}
		eligible, err := access_model.CanBeAssigned(owned, assignee, issue.Repo, false)
		if err != nil || !eligible {
			return refuse(model.ReasonNativeRefused)
		}
	}
	committed, err := model.CommitLifecycleIssuePrimary(owned, op.InstallationID, op.OperationID, owner, s.clock(), func(tx context.Context) (string, error) {
		receipt := LifecycleIssueReceipt{IssueID: submit.IssueID, LabelID: submit.LabelID, AssigneeID: submit.AssigneeID}
		if label != nil && !issues_model.HasIssueLabel(tx, submit.IssueID, submit.LabelID) {
			if err := issues_model.NewIssueLabel(tx, issue, label, doer); err != nil {
				return "", err
			}
			receipt.LabelAdded = true
		}
		if assignee != nil {
			assigned, err := issues_model.IsUserAssignedToIssue(tx, issue, assignee)
			if err != nil {
				return "", err
			}
			if !assigned {
				if _, _, err := issues_model.ToggleIssueAssignee(tx, issue, doer, submit.AssigneeID); err != nil {
					return "", err
				}
				receipt.AssigneeAdded = true
			}
		}
		encoded, _ := json.Marshal(receipt)
		return string(encoded), nil
	})
	if err != nil {
		switch {
		case errors.Is(err, model.ErrOperationExpired):
			return refuse(model.ReasonExpiredBeforeAdmission)
		case errors.Is(err, model.ErrAdmissionLost):
			current, lookupErr := model.LookupOperation(releaseCtx, op.InstallationID, op.OperationID)
			if lookupErr != nil {
				return sdk.OperationRecord{}, terminalResult{}, lookupErr
			}
			if current != nil && current.Revoked {
				return refuse(model.ReasonCancelledBeforeAdmission)
			}
			return sdk.OperationRecord{}, terminalResult{}, err
		default:
			return refuse(model.ReasonNativeRefused)
		}
	}
	op = committed
	if label != nil {
		notify_service.IssueChangeLabels(owned, doer, issue, []*issues_model.Label{label}, nil)
	}
	recorded, err := model.SetCompletionAndRelease(releaseCtx, op.InstallationID, op.OperationID, owner, execution.Generation, model.CompletionComplete)
	if err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	return ToRecord(recorded), terminalResult{}, nil
}
