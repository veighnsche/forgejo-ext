// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later
package nativeoperation

import (
	"context"
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	nativeoperation_model "forgejo.org/models/nativeoperation"
)

// SQL primary and ledger commit together. Recovery never adopts similar
// issues, replays a graph mutation or delivers notifications twice.
func (s *Service) recoverConditionalIssueGraph(ctx context.Context, a *RecoveryAssessment, r *nativeoperation_model.Reservation, scope Scope, op *nativeoperation_model.Operation) (RecoveryAssessment, error) {
	if scope.RepositoryID != op.RepositoryID {
		return fenced(a, ReasonRecoveryUncertainEffect, "graph scope and operation repository differ")
	}
	if !op.Admitted && op.EffectState == nativeoperation_model.EffectPending {
		if _, err := nativeoperation_model.SetTerminal(ctx, op.InstallationID, op.OperationID, nativeoperation_model.TerminalOutcome{EffectState: nativeoperation_model.EffectNotCommitted, Reason: nativeoperation_model.ReasonRecoveredNoEffect, Cancellation: nativeoperation_model.CancellationNone}, ""); err != nil {
			return RecoveryAssessment{}, err
		}
		return s.releaseRecovered(ctx, a, r, nativeoperation_model.EffectNotCommitted)
	}
	if !op.Admitted || op.EffectState != nativeoperation_model.EffectCommitted {
		return fenced(a, ReasonRecoveryUncertainEffect, "graph effect is not attributable")
	}
	var receipt sdk.IssueGraphReceipt
	if json.Unmarshal([]byte(op.Receipt), &receipt) != nil || receipt.ActorID != op.ActorID || receipt.RepositoryID != op.RepositoryID || len(receipt.Issues)+len(receipt.Dependencies) == 0 {
		return fenced(a, ReasonRecoveryUncertainEffect, "graph receipt is missing or inconsistent")
	}
	for _, entry := range receipt.Issues {
		issue, err := issues_model.GetIssueByID(ctx, entry.IssueID)
		if err != nil {
			return fenced(a, ReasonRecoveryMissingEvidence, "recorded graph issue cannot be read")
		}
		if issue.RepoID != receipt.RepositoryID || issue.IsPull || issue.Index != entry.IssueIndex || issue.ContentVersion != entry.ContentVersion || entry.Key != "" && issue.PosterID != receipt.ActorID {
			return fenced(a, ReasonRecoveryUncertainEffect, "recorded graph issue differs from native evidence")
		}
	}
	for _, edge := range receipt.Dependencies {
		exists, err := db.GetEngine(ctx).Where("issue_id=? AND dependency_id=?", edge.IssueID, edge.PrerequisiteID).Exist(new(issues_model.IssueDependency))
		if err != nil {
			return RecoveryAssessment{}, err
		}
		if exists == edge.Removed {
			return fenced(a, ReasonRecoveryUncertainEffect, "recorded dependency differs from native evidence")
		}
	}
	if op.Completion == nativeoperation_model.CompletionComplete || op.Completion == nativeoperation_model.CompletionNeedsIntervention {
		return s.releaseRecovered(ctx, a, r, nativeoperation_model.EffectCommitted)
	}
	if _, err := nativeoperation_model.SetCompletionAndRelease(ctx, op.InstallationID, op.OperationID, r.Owner, r.Generation, nativeoperation_model.CompletionNeedsIntervention); err != nil {
		return RecoveryAssessment{}, err
	}
	a.Verdict = RecoveryReleased
	a.Effect = nativeoperation_model.EffectCommitted
	a.Checks = append(a.Checks, "atomic graph receipt matches native records; notification completion requires intervention")
	s.retireStaleCapabilities()
	return *a, nil
}
