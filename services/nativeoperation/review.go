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
	"strings"

	sdk "forgejo.org/extension-sdk"
	authmodel "forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	execcontext "forgejo.org/modules/nativeoperation"
	api "forgejo.org/modules/structs"
	pull_service "forgejo.org/services/pull"
)

// ReviewSubmitIntent carries the validated pull_request.review.submit
// payload: one final body-only review bound to its exact candidate. The
// first interface covers same-repository PRs with commit_id equal to the
// expected head and a final event; inline comments, attachments,
// pending/draft creation, submission of an existing draft, editing and
// dismissal are outside this operation.
type ReviewSubmitIntent struct {
	PullRequestNumber int64  `json:"pull_request_number"`
	PRAuthorID        int64  `json:"pr_author_id"`
	HeadRepositoryID  int64  `json:"head_repository_id"`
	HeadRef           string `json:"head_ref"`
	BaseRef           string `json:"base_ref"`
	ExpectedHeadOID   string `json:"expected_head_oid"`
	ExpectedBaseOID   string `json:"expected_base_oid"`
	CommitID          string `json:"commit_id"`
	Event             string `json:"event"`
	Body              string `json:"body"`
}

// parseReviewSubmitPayload validates the complete immutable review-submit
// intent. Unknown fields, conflicting values and out-of-scope requests fail
// validation rather than silently broadening the request.
func parseReviewSubmitPayload(payload []byte, repositoryID int64) (*ReviewSubmitIntent, error) {
	if len(payload) == 0 {
		return nil, ErrInvalidIntent
	}
	var review ReviewSubmitIntent
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil {
		return nil, ErrInvalidIntent
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidIntent
	}
	if review.PullRequestNumber <= 0 || review.PRAuthorID <= 0 {
		return nil, ErrInvalidIntent
	}
	// The first interface submits reviews on same-repository PRs only.
	if review.HeadRepositoryID != repositoryID {
		return nil, ErrInvalidIntent
	}
	if !validBranchRef(review.HeadRef) || !validBranchRef(review.BaseRef) || review.HeadRef == review.BaseRef {
		return nil, ErrInvalidIntent
	}
	if !validOID(review.ExpectedHeadOID) || !validOID(review.ExpectedBaseOID) || !validOID(review.CommitID) {
		return nil, ErrInvalidIntent
	}
	// No omitted-commit default: the bound commit is always explicit and
	// must equal the expected head.
	if !strings.EqualFold(review.CommitID, review.ExpectedHeadOID) {
		return nil, ErrInvalidIntent
	}
	switch review.Event {
	case string(api.ReviewStateApproved):
	case string(api.ReviewStateRequestChanges):
		if strings.TrimSpace(review.Body) == "" {
			return nil, ErrInvalidIntent
		}
	default:
		return nil, ErrInvalidIntent
	}
	review.ExpectedHeadOID = strings.ToLower(review.ExpectedHeadOID)
	review.ExpectedBaseOID = strings.ToLower(review.ExpectedBaseOID)
	review.CommitID = strings.ToLower(review.CommitID)
	return &review, nil
}

// reviewSubmitType maps the validated final event to its native review type.
func reviewSubmitType(event string) issues_model.ReviewType {
	if event == string(api.ReviewStateRequestChanges) {
		return issues_model.ReviewTypeReject
	}
	return issues_model.ReviewTypeApprove
}

// ReviewSubmitReceipt attributes one committed review submission to its
// operation.
type ReviewSubmitReceipt struct {
	ReviewID   int64  `json:"review_id"`
	CommentID  int64  `json:"comment_id"`
	ReviewerID int64  `json:"reviewer_id"`
	PRID       int64  `json:"pr_id"`
	IssueID    int64  `json:"issue_id"`
	PRNumber   int64  `json:"pr_number"`
	CommitID   string `json:"commit_id"`
	Event      string `json:"event"`
	HeadOID    string `json:"head_oid"`
	BaseOID    string `json:"base_oid"`
}

// mapExactReviewRefusal maps a deterministic exact-review refusal to its
// bounded operation reason. Changed candidates refuse as stale; identity,
// source, target and lifecycle mismatches refuse as PR mismatches; denied
// native review eligibility refuses as authority lost; a conflicting pending
// draft refuses as pending. Anything unrecognized fails closed as a native
// refusal.
func mapExactReviewRefusal(err error) string {
	var refused issues_model.ErrExactReviewRefused
	if !errors.As(err, &refused) {
		if issues_model.IsContentEmptyErr(err) {
			return model.ReasonNativeRefused
		}
		return model.ReasonNativeRefused
	}
	switch refused.Reason {
	case "head changed":
		return model.ReasonStaleHead
	case "base changed":
		return model.ReasonStaleBaseOrResult
	case "pending draft exists":
		return model.ReasonPendingReviewExists
	case "cannot review your own pull request",
		"reviewer is blocked",
		"reviewer cannot read this pull request":
		return model.ReasonAuthorityLost
	case "invalid object id",
		"commit must equal the expected head",
		"only approve or reject reviews are supported":
		// Unreachable: intent parsing already rejects these. Fail
		// closed as a native refusal rather than a PR mismatch.
		return model.ReasonNativeRefused
	default:
		return model.ReasonPRMismatch
	}
}

// submitReviewSubmit claims the operation ID and reservation, verifies the
// exact refs and native authority under the held owner, commits the primary
// review records with the operation receipt in one SQL transaction, and
// completes mentions and notifications under bounded completion ownership.
// There is no unguarded fallback: every refusal path records not_committed
// or retains the fence.
func (s *Service) submitReviewSubmit(ctx context.Context, decision authmodel.SubmissionDecision, installationID string, intent *ValidIntent) (sdk.OperationRecord, error) {
	if err := ctx.Err(); err != nil {
		return sdk.OperationRecord{}, err
	}
	if intent.ReviewSubmit == nil {
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
		Kind:         model.OwnerConditional,
		RepositoryID: intent.RepositoryID,
		Ref:          intent.ReviewSubmit.BaseRef,
		OldOID:       intent.ReviewSubmit.ExpectedBaseOID,
		HeadRef:      intent.ReviewSubmit.HeadRef,
		HeadOID:      intent.ReviewSubmit.ExpectedHeadOID,
		PRNumber:     intent.ReviewSubmit.PullRequestNumber,
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
	record, terminal, err := s.executeReviewSubmit(ctx, op, intent, execution)
	if err != nil {
		// Infrastructure failure before reconciliation: fail closed with
		// the owner held and the capability kept for later recovery.
		return sdk.OperationRecord{}, err
	}
	if !terminal.retain {
		// The terminal record already released the owner; retire the file.
		retire()
	}
	return record, nil
}

// executeReviewSubmit runs the guarded submission and reconciles its result.
// It returns the SDK record and whether the owner stays held for an
// unresolved effect. All native work runs on the owned context so nested
// participating writers attribute to this execution; terminal records and
// the owner release run detached so a caller disconnect cannot stick the
// reservation.
func (s *Service) executeReviewSubmit(ctx context.Context, op *model.Operation, intent *ValidIntent, execution *execcontext.Execution) (sdk.OperationRecord, terminalResult, error) {
	owner := execution.Owner
	submit := intent.ReviewSubmit
	// Terminal records and the owner release must complete even when the
	// caller disconnects mid-write; native work below keeps the live
	// context so cancellation still stops it.
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
	// The exact tips are read under the held reservation; the atomic
	// primary re-verifies them without touching Git, and refuses on any
	// change with no diff-equivalence substitution.
	head, err := s.readRef(owned, repository.RepoPath(), submit.HeadRef)
	if err != nil || !strings.EqualFold(head, submit.ExpectedHeadOID) {
		return refuse(model.ReasonStaleHead)
	}
	base, err := s.readRef(owned, repository.RepoPath(), submit.BaseRef)
	if err != nil || !strings.EqualFold(base, submit.ExpectedBaseOID) {
		return refuse(model.ReasonStaleBaseOrResult)
	}
	doer, err := user_model.GetUserByID(owned, op.ActorID)
	if err != nil {
		return refuse(model.ReasonAuthorityLost)
	}
	pr, err := issues_model.GetPullRequestByIndex(owned, op.RepositoryID, submit.PullRequestNumber)
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			return refuse(model.ReasonPRMismatch)
		}
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	if err := pr.LoadIssue(owned); err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	issue := pr.Issue
	issue.PullRequest = pr
	target := issues_model.ExactReviewTarget{
		PRIndex:        submit.PullRequestNumber,
		AuthorID:       submit.PRAuthorID,
		HeadRepoID:     submit.HeadRepositoryID,
		HeadBranch:     strings.TrimPrefix(submit.HeadRef, git.BranchPrefix),
		BaseBranch:     strings.TrimPrefix(submit.BaseRef, git.BranchPrefix),
		HeadOID:        submit.ExpectedHeadOID,
		BaseOID:        submit.ExpectedBaseOID,
		CommitID:       submit.CommitID,
		CurrentHeadOID: strings.ToLower(head),
		CurrentBaseOID: strings.ToLower(base),
	}
	reviewType := reviewSubmitType(submit.Event)

	// Test-only barrier for the transaction/cancel race proof: with the
	// claim held, the driver revokes the operation here so the atomic
	// primary commit below must observe the cancellation and refuse.
	if err := TestCrashBarrier(CrashPointReviewSubmitBeforePrimary); err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}

	// Primary effect: the native review/comment rows with official-state
	// and request changes, plus the operation's committed receipt, in one
	// SQL transaction ordered against cancellation and expiry on the same
	// operation row. No SQL transaction spans Git or wraps the service.
	// A pending draft for this actor refuses without adoption or deletion.
	var review *issues_model.Review
	var comm *issues_model.Comment
	committed, err := model.CommitReviewSubmitPrimary(owned, op.InstallationID, op.OperationID, owner, s.clock(), func(tx context.Context) (string, error) {
		submitted, comment, err := issues_model.SubmitExactReview(tx, doer, issue, reviewType, submit.Body, target)
		if err != nil {
			return "", err
		}
		review, comm = submitted, comment
		receipt, _ := json.Marshal(ReviewSubmitReceipt{
			ReviewID:   submitted.ID,
			CommentID:  comment.ID,
			ReviewerID: doer.ID,
			PRID:       pr.ID,
			IssueID:    issue.ID,
			PRNumber:   submit.PullRequestNumber,
			CommitID:   submit.CommitID,
			Event:      submit.Event,
			HeadOID:    submit.ExpectedHeadOID,
			BaseOID:    submit.ExpectedBaseOID,
		})
		return string(receipt), nil
	})
	if err != nil {
		switch {
		case errors.Is(err, model.ErrOperationExpired):
			return refuse(model.ReasonExpiredBeforeAdmission)
		case errors.Is(err, model.ErrAdmissionLost):
			// A lost primary commit rereads the ordering truth: a
			// concurrent cancellation wins prevention, anything else
			// fails closed with the owner held.
			current, lookupErr := model.LookupOperation(releaseCtx, op.InstallationID, op.OperationID)
			if lookupErr != nil {
				return sdk.OperationRecord{}, terminalResult{}, lookupErr
			}
			if current != nil && current.Revoked {
				return refuse(model.ReasonCancelledBeforeAdmission)
			}
			return sdk.OperationRecord{}, terminalResult{}, err
		default:
			return refuse(mapExactReviewRefusal(err))
		}
	}
	op = committed

	// Test-only crash barrier for offline-recovery proof: with the
	// primary records committed and the owner still held, the driver
	// fails here to simulate a crash before bounded completion.
	if err := TestCrashBarrier(CrashPointReviewSubmitAfterPrimary); err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}

	// Bounded completion under the held owner: mentions and notifications
	// share the ordinary completion. A review can be durable while its
	// completion is incomplete: completion failure reports
	// needs_intervention without rolling the committed primary back.
	// There is no derived Git write, so no completion tuple persists.
	if err := pull_service.CompleteReviewSubmission(owned, doer, issue, review, comm); err != nil {
		return s.finalizeReviewSubmit(releaseCtx, op, execution, model.CompletionNeedsIntervention)
	}
	return s.finalizeReviewSubmit(releaseCtx, op, execution, model.CompletionComplete)
}

// finalizeReviewSubmit records the completion state of one committed
// submission and releases its exact owner. A finalize failure keeps the
// owner held for offline recovery; it never downgrades the committed
// primary.
func (s *Service) finalizeReviewSubmit(ctx context.Context, op *model.Operation, execution *execcontext.Execution, completion string) (sdk.OperationRecord, terminalResult, error) {
	recorded, err := model.SetCompletionAndRelease(ctx, op.InstallationID, op.OperationID, execution.Owner, execution.Generation, completion)
	if err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	return ToRecord(recorded), terminalResult{}, nil
}
