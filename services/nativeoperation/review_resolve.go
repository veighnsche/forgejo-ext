// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"bytes"
	"context"
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"errors"
	"io"
	"os"
	"strings"

	sdk "forgejo.org/extension-sdk"
	extensionauth_model "forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	nativeoperation_model "forgejo.org/models/nativeoperation"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	execcontext "forgejo.org/modules/nativeoperation"
)

// Conditional thread resolution (D06, FW17).
//
// One pull_request.review.thread.resolve operation resolves the anchored
// native thread root bound to its exact PR/head/base and the expected
// complete conversation digest. The host re-derives the native root from
// the shared grouping, recomputes the digest from live state and resolves
// through CanMarkConversation/MarkConversation under the enrolled resolve
// binding. A missing binding, wrong actor, non-root or deleted target, or
// changed content refuses with no partial effect; identical replay
// returns the recorded receipt.

// parseThreadResolvePayload validates the complete immutable
// thread-resolve intent. Unknown fields and out-of-scope requests fail
// validation rather than silently broadening the request.
func parseThreadResolvePayload(payload []byte, repositoryID int64) (*sdk.ThreadResolvePayload, error) {
	if len(payload) == 0 {
		return nil, ErrInvalidIntent
	}
	var resolve sdk.ThreadResolvePayload
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&resolve); err != nil {
		return nil, ErrInvalidIntent
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidIntent
	}
	if err := sdk.ValidateThreadResolvePayload(repositoryID, resolve); err != nil {
		return nil, ErrInvalidIntent
	}
	resolve.ExpectedHeadOID = strings.ToLower(resolve.ExpectedHeadOID)
	resolve.ExpectedBaseOID = strings.ToLower(resolve.ExpectedBaseOID)
	return &resolve, nil
}

// submitThreadResolve claims the operation ID and reservation, verifies
// the exact refs and resolve authority under the held owner, commits the
// resolution with the operation receipt in one SQL transaction, and
// releases the owner. There is no unguarded fallback: every refusal path
// records not_committed or retains the fence.
func (s *Service) submitThreadResolve(ctx context.Context, decision extensionauth_model.SubmissionDecision, installationID string, intent *ValidIntent) (sdk.OperationRecord, error) {
	if err := ctx.Err(); err != nil {
		return sdk.OperationRecord{}, err
	}
	if intent.ThreadResolve == nil {
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
		Kind:         nativeoperation_model.OwnerConditional,
		RepositoryID: intent.RepositoryID,
		Ref:          intent.ThreadResolve.BaseRef,
		OldOID:       intent.ThreadResolve.ExpectedBaseOID,
		HeadRef:      intent.ThreadResolve.HeadRef,
		HeadOID:      intent.ThreadResolve.ExpectedHeadOID,
		PRNumber:     intent.ThreadResolve.PullRequestNumber,
	}
	encoded, err := encodeScope(scope)
	if err != nil {
		retire()
		return sdk.OperationRecord{}, err
	}
	op := &nativeoperation_model.Operation{
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
	claimed, err := nativeoperation_model.ClaimConditional(ctx, op, owner, encoded, execcontext.Verifier(secret))
	if err != nil {
		retire()
		if errors.Is(err, nativeoperation_model.ErrBusy) {
			return sdk.OperationRecord{}, ErrBusy
		}
		if errors.Is(err, nativeoperation_model.ErrInhibited) {
			return sdk.OperationRecord{}, err
		}
		if errors.Is(err, nativeoperation_model.ErrStaleRevision) {
			return s.recordStaleRevision(ctx, op)
		}
		// A concurrent identical submit may have won the insert; reread
		// and reconcile rather than launching a second execution.
		existing, lookupErr := nativeoperation_model.LookupOperation(ctx, installationID, intent.OperationID)
		if lookupErr != nil {
			return sdk.OperationRecord{}, lookupErr
		}
		if existing != nil {
			return s.reconcileExisting(existing, intent)
		}
		return sdk.OperationRecord{}, err
	}
	execution := &execcontext.Execution{Owner: owner, Generation: claimed.Generation, CapabilityPath: capabilityPath}
	record, terminal, err := s.executeThreadResolve(ctx, op, intent, execution)
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

// executeThreadResolve runs the guarded resolution and reconciles its
// result. It returns the SDK record and whether the owner stays held for
// an unresolved effect. All native work runs on the owned context so the
// nested resolve attributes to this execution; terminal records and the
// owner release run detached so a caller disconnect cannot stick the
// reservation.
func (s *Service) executeThreadResolve(ctx context.Context, op *nativeoperation_model.Operation, intent *ValidIntent, execution *execcontext.Execution) (sdk.OperationRecord, terminalResult, error) {
	owner := execution.Owner
	resolve := intent.ThreadResolve
	// Terminal records and the owner release must complete even when the
	// caller disconnects mid-write; native work below keeps the live
	// context so cancellation still stops it.
	releaseCtx := context.WithoutCancel(ctx)
	owned := execcontext.NewContext(ctx, execution)
	refuse := func(reason string) (sdk.OperationRecord, terminalResult, error) {
		recorded, err := nativeoperation_model.SetTerminal(releaseCtx, op.InstallationID, op.OperationID, nativeoperation_model.TerminalOutcome{
			EffectState:  nativeoperation_model.EffectNotCommitted,
			Reason:       reason,
			Cancellation: nativeoperation_model.CancellationNone,
		}, owner)
		if err != nil {
			return sdk.OperationRecord{}, terminalResult{}, err
		}
		return ToRecord(recorded), terminalResult{}, nil
	}
	if err := s.revalidateSubmission(owned, op); err != nil {
		if errors.Is(err, ErrAuthorityLost) {
			return refuse(nativeoperation_model.ReasonAuthorityLost)
		}
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	repository, err := repo_model.GetRepositoryByID(owned, op.RepositoryID)
	if err != nil {
		return refuse(nativeoperation_model.ReasonNativeRefused)
	}
	if s.clock() >= op.NotAfter {
		return refuse(nativeoperation_model.ReasonExpiredBeforeAdmission)
	}
	// The exact tips are read under the held reservation; resolution
	// refuses on any change with no diff-equivalence substitution.
	head, err := s.readRef(owned, repository.RepoPath(), resolve.HeadRef)
	if err != nil || !strings.EqualFold(head, resolve.ExpectedHeadOID) {
		return refuse(nativeoperation_model.ReasonStaleHead)
	}
	base, err := s.readRef(owned, repository.RepoPath(), resolve.BaseRef)
	if err != nil || !strings.EqualFold(base, resolve.ExpectedBaseOID) {
		return refuse(nativeoperation_model.ReasonStaleBaseOrResult)
	}
	doer, err := user_model.GetUserByID(owned, op.ActorID)
	if err != nil {
		return refuse(nativeoperation_model.ReasonAuthorityLost)
	}
	pr, err := issues_model.GetPullRequestByIndex(owned, op.RepositoryID, resolve.PullRequestNumber)
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			return refuse(nativeoperation_model.ReasonPRMismatch)
		}
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	if err := pr.LoadIssue(owned); err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	if pr.HeadRepoID != resolve.HeadRepositoryID {
		return refuse(nativeoperation_model.ReasonPRMismatch)
	}
	issue := pr.Issue
	// Resolve authority uses the reviewer account identity with its own
	// operation authorization; review permission is not a reusable
	// resolve credential. No administrator bypass.
	allowed, err := issues_model.CanMarkConversation(owned, issue, doer)
	if err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	if !allowed {
		return refuse(nativeoperation_model.ReasonAuthorityLost)
	}
	// Native root derivation: the named root must be a live code comment
	// on this issue and the first comment of its grouped conversation.
	// Replies are members, never resolution targets; a deleted or
	// foreign root cannot transfer its mapping.
	named, err := issues_model.GetCommentByID(owned, resolve.RootCommentID)
	if err != nil {
		if issues_model.IsErrCommentNotExist(err) {
			return refuse(nativeoperation_model.ReasonPRMismatch)
		}
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	if named.Type != issues_model.CommentTypeCode || named.IssueID != issue.ID {
		return refuse(nativeoperation_model.ReasonPRMismatch)
	}
	liveDigest, root, err := liveConversationDigest(owned, issue, named)
	if err != nil {
		if errors.Is(err, errResolveTarget) {
			return refuse(nativeoperation_model.ReasonPRMismatch)
		}
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	if root.ID != named.ID {
		// The named comment is a reply, not the anchored root.
		return refuse(nativeoperation_model.ReasonPRMismatch)
	}
	if !strings.EqualFold(liveDigest, resolve.ConversationDigest) {
		// A reply, edit, unresolve or resolver-state change since the
		// snapshot refuses the stale resolution.
		return refuse(nativeoperation_model.ReasonStaleConversation)
	}
	// Primary effect: the native resolve-doer mark plus the operation's
	// committed receipt, in one SQL transaction ordered against
	// cancellation and expiry on the same operation row. Re-resolution
	// of an already-resolved root is stable: no error, no ownership
	// change, and the receipt replays exactly.
	committed, err := nativeoperation_model.CommitReviewSubmitPrimary(owned, op.InstallationID, op.OperationID, owner, s.clock(), func(tx context.Context) (string, error) {
		if err := issues_model.MarkConversation(tx, root, doer, true); err != nil {
			return "", err
		}
		receipt, _ := json.Marshal(sdk.ThreadResolveReceipt{
			ConversationDigest: strings.ToLower(resolve.ConversationDigest),
			PullRequestNumber:  resolve.PullRequestNumber,
			RootCommentID:      resolve.RootCommentID,
			Resolved:           true,
		})
		return string(receipt), nil
	})
	if err != nil {
		switch {
		case errors.Is(err, nativeoperation_model.ErrOperationExpired):
			return refuse(nativeoperation_model.ReasonExpiredBeforeAdmission)
		case errors.Is(err, nativeoperation_model.ErrAdmissionLost):
			// A lost primary commit rereads the ordering truth: a
			// concurrent cancellation wins prevention, anything else
			// fails closed with the owner held.
			current, lookupErr := nativeoperation_model.LookupOperation(releaseCtx, op.InstallationID, op.OperationID)
			if lookupErr != nil {
				return sdk.OperationRecord{}, terminalResult{}, lookupErr
			}
			if current != nil && current.Revoked {
				return refuse(nativeoperation_model.ReasonCancelledBeforeAdmission)
			}
			return sdk.OperationRecord{}, terminalResult{}, err
		default:
			return refuse(nativeoperation_model.ReasonNativeRefused)
		}
	}
	op = committed
	return s.finalizeThreadResolve(releaseCtx, op, execution, nativeoperation_model.CompletionComplete)
}

// errResolveTarget reports a resolve target outside any live grouped
// conversation: a reply without a group, an unanchored comment, or a
// comment the grouping cannot represent.
var errResolveTarget = errors.New("thread-resolve target is not a conversation root")

// liveConversationDigest re-derives the native root and the complete
// conversation digest from live state through the shared grouping, using
// the root anchor for every member line exactly as the snapshot loader
// does. Any divergence from the snapshot digest refuses the resolution.
func liveConversationDigest(ctx context.Context, issue *issues_model.Issue, named *issues_model.Comment) (string, *issues_model.Comment, error) {
	comments, _, err := issues_model.FetchThreadComments(ctx, issue)
	if err != nil {
		return "", nil, err
	}
	thread := issues_model.FindCodeThread(issues_model.GroupCodeThreads(comments), named.ID)
	if thread == nil {
		return "", nil, errResolveTarget
	}
	root := thread.Root()
	var side string
	var unsigned int64
	switch {
	case root.Line < 0:
		side, unsigned = "old", -root.Line
	case root.Line > 0:
		side, unsigned = "new", root.Line
	default:
		return "", nil, errResolveTarget
	}
	location := sdk.RenderThreadLocation(root.TreePath, side, unsigned)
	lines := make([]string, 0, len(thread))
	for _, member := range thread {
		if member == nil {
			continue
		}
		lines = append(lines, sdk.ResolveConversationLine(sdk.ResolveConversationMember{
			ID:            member.ID,
			Body:          member.Content,
			ContentVer:    int64(member.ContentVersion),
			Location:      location,
			ResolverState: sdk.ThreadResolverState(member.ResolveDoerID),
		}))
	}
	digest, err := sdk.ResolveConversationDigest(lines)
	if err != nil {
		return "", nil, err
	}
	return digest, root, nil
}

// finalizeThreadResolve records the completion state of one committed
// resolution and releases its exact owner. A finalize failure keeps the
// owner held for offline recovery; it never downgrades the committed
// primary.
func (s *Service) finalizeThreadResolve(ctx context.Context, op *nativeoperation_model.Operation, execution *execcontext.Execution, completion string) (sdk.OperationRecord, terminalResult, error) {
	recorded, err := nativeoperation_model.SetCompletionAndRelease(ctx, op.InstallationID, op.OperationID, execution.Owner, execution.Generation, completion)
	if err != nil {
		return sdk.OperationRecord{}, terminalResult{}, err
	}
	return ToRecord(recorded), terminalResult{}, nil
}
