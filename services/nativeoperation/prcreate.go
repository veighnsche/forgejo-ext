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
	"forgejo.org/models/db"
	authmodel "forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	execcontext "forgejo.org/modules/nativeoperation"
	issue_service "forgejo.org/services/issue"
	notify_service "forgejo.org/services/notify"
	pull_service "forgejo.org/services/pull"
)

// DRAFT (FT12 early start): conditional pull_request.create against the
// committed operation seam. Wiring into ValidateIntent/Submit, the atomic
// primary-commit helper and the PR-create refusal reasons are held for the
// shared seam items; execution-proof plumbing for completion Git and the
// offline kind-specific recovery belong to the later operation proof. This
// file compiles and its pure validation is tested, but no dispatcher reaches
// it yet and its DRAFT limitations below stay open until the seam lands.

// PRCreateIntent carries the validated pull_request.create payload: one
// same-repository native PR over exact head/base refs and OIDs with final
// title and body. The first interface has no labels, assignees, milestone,
// attachments, fork source, update or retarget operation.
type PRCreateIntent struct {
	HeadRepositoryID    int64  `json:"head_repository_id"`
	HeadRef             string `json:"head_ref"`
	BaseRef             string `json:"base_ref"`
	ExpectedHeadOID     string `json:"expected_head_oid"`
	ExpectedBaseOID     string `json:"expected_base_oid"`
	Title               string `json:"title"`
	Body                string `json:"body"`
	AllowMaintainerEdit bool   `json:"allow_maintainer_edit"`
}

// maxPRCreateTitle is the native issue-title width. Over-limit titles reject
// rather than silently truncating the intent.
const maxPRCreateTitle = 255

// parsePRCreatePayload validates the complete immutable PR-create intent.
// Unknown fields, conflicting values and out-of-scope requests fail validation
// rather than silently broadening the request.
func parsePRCreatePayload(payload []byte, repositoryID int64) (*PRCreateIntent, error) {
	if len(payload) == 0 {
		return nil, ErrInvalidIntent
	}
	var pr PRCreateIntent
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pr); err != nil {
		return nil, ErrInvalidIntent
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidIntent
	}
	// The first interface creates same-repository PRs only.
	if pr.HeadRepositoryID != repositoryID {
		return nil, ErrInvalidIntent
	}
	if !validBranchRef(pr.HeadRef) || !validBranchRef(pr.BaseRef) || pr.HeadRef == pr.BaseRef {
		return nil, ErrInvalidIntent
	}
	if !validOID(pr.ExpectedHeadOID) || !validOID(pr.ExpectedBaseOID) {
		return nil, ErrInvalidIntent
	}
	if strings.EqualFold(pr.ExpectedHeadOID, pr.ExpectedBaseOID) {
		return nil, ErrInvalidIntent
	}
	pr.ExpectedHeadOID = strings.ToLower(pr.ExpectedHeadOID)
	pr.ExpectedBaseOID = strings.ToLower(pr.ExpectedBaseOID)
	pr.Title = strings.TrimSpace(pr.Title)
	if pr.Title == "" || len(pr.Title) > maxPRCreateTitle {
		return nil, ErrInvalidIntent
	}
	if pr.AllowMaintainerEdit {
		return nil, ErrInvalidIntent
	}
	return &pr, nil
}

// PRCreateReceipt attributes one committed PR creation to its operation.
type PRCreateReceipt struct {
	PRID         int64  `json:"pr_id"`
	IssueID      int64  `json:"issue_id"`
	PRNumber     int64  `json:"pr_number"`
	AuthorID     int64  `json:"author_id"`
	RepositoryID int64  `json:"repository_id"`
	HeadRef      string `json:"head_ref"`
	BaseRef      string `json:"base_ref"`
	HeadOID      string `json:"head_oid"`
	BaseOID      string `json:"base_oid"`
}

// errPRCreateRevoked and errPRCreateExpired roll the draft's primary SQL
// attempt back so the caller can refuse without a partial effect.
var (
	errPRCreateRevoked = errors.New("operation revoked before primary commit")
	errPRCreateExpired = errors.New("operation expired before primary commit")
)

// submitPRCreate claims the operation ID and reservation, verifies the exact
// refs and native authority under the held owner, commits the primary issue/PR
// records with the operation receipt, and completes the derived ref and
// follow-up records. DRAFT entry point: it takes explicit identity fields
// until ValidateIntent carries PRCreateIntent and Submit dispatches this kind.
// There is no unguarded fallback: every refusal path records not_committed or
// retains the fence.
func (s *Service) submitPRCreate(ctx context.Context, decision authmodel.SubmissionDecision, installationID, operationID, authRevision string, expectedRevision, notAfter int64, digest, canonical string, pr *PRCreateIntent) (sdk.OperationRecord, error) {
	dir, err := s.execDir()
	if err != nil {
		return sdk.OperationRecord{}, err
	}
	capabilityPath, secret, err := execcontext.WriteCapabilityFile(dir)
	if err != nil {
		return sdk.OperationRecord{}, err
	}
	retire := func() { _ = os.Remove(capabilityPath) }
	owner := conditionalOwner(installationID, operationID)
	// DRAFT scope: the committed Scope has no PR-create internal-ref tuple
	// yet, so bind the verified head/base refs and OIDs with the existing
	// fields. The final version persists the exact allocated internal ref
	// under this owner before derived Git writes.
	scope := Scope{
		Kind:         model.OwnerConditional,
		RepositoryID: pr.HeadRepositoryID,
		Ref:          pr.BaseRef,
		OldOID:       pr.ExpectedBaseOID,
		HeadRef:      pr.HeadRef,
		HeadOID:      pr.ExpectedHeadOID,
	}
	encoded, err := encodeScope(scope)
	if err != nil {
		retire()
		return sdk.OperationRecord{}, err
	}
	op := &model.Operation{
		InstallationID:         installationID,
		OperationID:            operationID,
		Kind:                   model.KindPRCreate,
		ActorID:                decision.ActorID,
		RepositoryID:           pr.HeadRepositoryID,
		TokenID:                decision.TokenID,
		CredentialFingerprint:  decision.CredentialFingerprint,
		AuthRevision:           authRevision,
		ExpectedNativeRevision: expectedRevision,
		NotAfter:               notAfter,
		IntentDigest:           digest,
		Intent:                 canonical,
	}
	claimed, err := model.ClaimConditional(ctx, op, owner, encoded, execcontext.Verifier(secret))
	if err != nil {
		retire()
		if errors.Is(err, model.ErrBusy) {
			return sdk.OperationRecord{}, ErrBusy
		}
		if errors.Is(err, model.ErrStaleRevision) {
			op.EffectState = model.EffectNotCommitted
			op.Reason = model.ReasonStaleNativeRevision
			op.Cancellation = model.CancellationNone
			recorded, err := model.InsertRefusedOperation(ctx, op)
			if err != nil && !errors.Is(err, model.ErrDuplicateOperation) {
				return sdk.OperationRecord{}, err
			}
			return ToRecord(recorded), nil
		}
		// A concurrent identical submit may have won the insert; reread
		// and reconcile rather than launching a second execution.
		existing, lookupErr := model.LookupOperation(ctx, installationID, operationID)
		if lookupErr != nil {
			return sdk.OperationRecord{}, lookupErr
		}
		if existing != nil {
			if !existing.Submitted {
				return ToRecord(existing), ErrCancelledBeforeSubmit
			}
			if existing.IntentDigest != digest {
				return sdk.OperationRecord{}, ErrIntentConflict
			}
			return ToRecord(existing), nil
		}
		return sdk.OperationRecord{}, err
	}
	execution := &execcontext.Execution{Owner: owner, Generation: claimed.Generation, CapabilityPath: capabilityPath}
	record, retain, err := s.executePRCreate(ctx, op, pr, execution)
	if err != nil {
		// Infrastructure failure before reconciliation: fail closed with
		// the owner held and the capability kept for later recovery.
		return sdk.OperationRecord{}, err
	}
	if !retain {
		// SetTerminal already released the owner; retire the file.
		retire()
	}
	return record, nil
}

// executePRCreate runs the guarded creation and reconciles its result. It
// returns the SDK record and whether the owner stays held for an unresolved
// effect.
func (s *Service) executePRCreate(ctx context.Context, op *model.Operation, pr *PRCreateIntent, execution *execcontext.Execution) (sdk.OperationRecord, bool, error) {
	owner := execution.Owner
	refuse := func(reason string) (sdk.OperationRecord, bool, error) {
		recorded, err := model.SetTerminal(ctx, op.InstallationID, op.OperationID, model.TerminalOutcome{
			EffectState:  model.EffectNotCommitted,
			Reason:       reason,
			Cancellation: model.CancellationNone,
		}, owner)
		if err != nil {
			return sdk.OperationRecord{}, false, err
		}
		return ToRecord(recorded), false, nil
	}
	if err := s.revalidateSubmission(ctx, op); err != nil {
		if errors.Is(err, ErrAuthorityLost) {
			return refuse(model.ReasonAuthorityLost)
		}
		return sdk.OperationRecord{}, false, err
	}
	repository, err := repo_model.GetRepositoryByID(ctx, op.RepositoryID)
	if err != nil {
		return refuse(model.ReasonNativeRefused)
	}
	if s.clock() >= op.NotAfter {
		return refuse(model.ReasonExpiredBeforeAdmission)
	}
	head, err := s.readRef(ctx, repository.RepoPath(), pr.HeadRef)
	if err != nil || !strings.EqualFold(head, pr.ExpectedHeadOID) {
		return refuse(model.ReasonStaleHead)
	}
	base, err := s.readRef(ctx, repository.RepoPath(), pr.BaseRef)
	if err != nil || !strings.EqualFold(base, pr.ExpectedBaseOID) {
		return refuse(model.ReasonStaleBaseOrResult)
	}
	doer, err := user_model.GetUserByID(ctx, op.ActorID)
	if err != nil {
		return refuse(model.ReasonAuthorityLost)
	}
	if user_model.IsBlocked(ctx, repository.OwnerID, doer.ID) {
		return refuse(model.ReasonAuthorityLost)
	}
	permission, err := access_model.GetUserRepoPermission(ctx, repository, doer)
	if err != nil {
		return refuse(model.ReasonAuthorityLost)
	}
	if !permission.CanWrite(unit.TypeCode) {
		return refuse(model.ReasonAuthorityLost)
	}
	headBranch := strings.TrimPrefix(pr.HeadRef, git.BranchPrefix)
	baseBranch := strings.TrimPrefix(pr.BaseRef, git.BranchPrefix)
	existing, err := issues_model.GetUnmergedPullRequest(ctx, op.RepositoryID, op.RepositoryID, headBranch, baseBranch, issues_model.PullRequestFlowGithub)
	if err != nil && !issues_model.IsErrPullRequestNotExist(err) {
		return sdk.OperationRecord{}, false, err
	}
	if existing != nil {
		// An existing PR is a conflict even when its author, branch or
		// text matches; only this operation's receipt proves creation.
		// DRAFT: reuse the native refusal reason until the seam adds a
		// duplicate-PR reason.
		return refuse(model.ReasonNativeRefused)
	}

	issue := &issues_model.Issue{
		RepoID:   op.RepositoryID,
		Title:    pr.Title,
		PosterID: doer.ID,
		Poster:   doer,
		IsPull:   true,
		Content:  pr.Body,
	}
	pull := &issues_model.PullRequest{
		HeadRepoID: op.RepositoryID,
		BaseRepoID: op.RepositoryID,
		HeadBranch: headBranch,
		BaseBranch: baseBranch,
		HeadRepo:   repository,
		BaseRepo:   repository,
		Type:       issues_model.PullRequestGitea,
	}

	// Primary effect: the native issue/PR rows. DRAFT: the committed seam
	// records the operation receipt in a separate SetTerminal, so this draft
	// rechecks revocation/expiry immediately before the insert and commits
	// the receipt right after. The final version folds the cancellation
	// contention and the committed receipt into this same SQL transaction
	// through the atomic primary-commit helper (held seam item).
	primaryErr := db.WithTx(ctx, func(ctx context.Context) error {
		current, err := model.LookupOperation(ctx, op.InstallationID, op.OperationID)
		if err != nil {
			return err
		}
		if current == nil || current.Revoked {
			return errPRCreateRevoked
		}
		if s.clock() >= current.NotAfter {
			return errPRCreateExpired
		}
		if err := issues_model.NewPullRequest(ctx, repository, issue, nil, nil, pull); err != nil {
			return err
		}
		pull.Issue = issue
		issue.PullRequest = pull
		return nil
	})
	if primaryErr != nil {
		if errors.Is(primaryErr, errPRCreateRevoked) {
			return refuse(model.ReasonCancelledBeforeAdmission)
		}
		if errors.Is(primaryErr, errPRCreateExpired) {
			return refuse(model.ReasonExpiredBeforeAdmission)
		}
		return refuse(model.ReasonNativeRefused)
	}

	// Bounded completion under the held owner: the exact internal PR ref,
	// push-history comment, code-owner requests, mentions and notifications
	// share the ordinary completion. DRAFT: the execution is carried in the
	// context as for merge, but the completion Git calls do not yet propagate
	// the execution proof to the reference-transaction hook; that plumbing
	// and the persisted internal-ref tuple land with the seam.
	owned := execcontext.NewContext(ctx, execution)
	completion := model.CompletionComplete
	notifiers, err := pull_service.CompletePullRequestCreation(owned, repository, pull)
	if err != nil {
		completion = model.CompletionNeedsIntervention
	} else {
		issue_service.ReviewRequestNotify(owned, issue, issue.Poster, notifiers)
		mentions, err := issues_model.FindAndUpdateIssueMentions(owned, issue, issue.Poster, issue.Content)
		if err != nil {
			completion = model.CompletionNeedsIntervention
		} else {
			notify_service.NewPullRequest(owned, pull, mentions)
		}
	}

	receipt, _ := json.Marshal(PRCreateReceipt{
		PRID:         pull.ID,
		IssueID:      issue.ID,
		PRNumber:     pull.Index,
		AuthorID:     doer.ID,
		RepositoryID: op.RepositoryID,
		HeadRef:      pr.HeadRef,
		BaseRef:      pr.BaseRef,
		HeadOID:      pr.ExpectedHeadOID,
		BaseOID:      pr.ExpectedBaseOID,
	})
	recorded, err := model.SetTerminal(ctx, op.InstallationID, op.OperationID, model.TerminalOutcome{
		EffectState:  model.EffectCommitted,
		Cancellation: model.CancellationNone,
		Completion:   completion,
		Receipt:      string(receipt),
	}, owner)
	if err != nil {
		return sdk.OperationRecord{}, false, err
	}
	return ToRecord(recorded), false, nil
}
