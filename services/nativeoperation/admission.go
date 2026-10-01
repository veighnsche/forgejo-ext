// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	execcontext "forgejo.org/modules/nativeoperation"
)

// ErrAuthorityLost reports that the bound credential generation, binding or
// native authority no longer validates against authoritative state.
var ErrAuthorityLost = errors.New("authority_lost")

// RefLine is one reference-transaction input line.
type RefLine struct {
	Old string
	New string
	Ref string
}

// Reference-transaction hook phases.
const (
	PhasePrepared  = "prepared"
	PhaseCommitted = "committed"
	PhaseAborted   = "aborted"
)

// TransactionRequest classifies one reference-transaction callback from a
// generated native hook.
type TransactionRequest struct {
	OwnerName string
	RepoName  string
	Phase     string
	Lines     []RefLine
	Proof     string
}

// TransactionDecision is the hook verdict. Refusals carry a bounded reason
// safe for hook output; secrets never appear.
type TransactionDecision struct {
	Allowed bool
	Reason  string
}

// GateState reports whether the reservation is idle, for hook callbacks that
// carry no repository environment. Held means every participating writer is
// fenced until the owner resolves.
func (s *Service) GateState(ctx context.Context) (bool, error) {
	reservation, err := model.ReadReservation(ctx)
	if err != nil {
		return false, err
	}
	return reservation.Owner == "", nil
}

// ClassifyTransaction enforces the reservation at Git's
// reference-transaction checkpoint. An idle reservation allows the ordinary
// unintegrated path. A held reservation requires the owner's execution proof
// and effects within its recorded scope; a conditional merge additionally
// passes prepared admission. Unknown infrastructure failures return an error
// and the hook fails closed.
func (s *Service) ClassifyTransaction(ctx context.Context, req TransactionRequest) (TransactionDecision, error) {
	reservation, err := model.ReadReservation(ctx)
	if err != nil {
		return TransactionDecision{}, err
	}
	if reservation.Owner == "" {
		return TransactionDecision{Allowed: true}, nil
	}
	if !execcontext.VerifyProof(reservation.Verifier, req.Proof) {
		return TransactionDecision{Reason: "native operation in progress"}, nil
	}
	scope, err := decodeScope(reservation.Scope)
	if err != nil {
		return TransactionDecision{}, err
	}
	repository, err := repo_model.GetRepositoryByOwnerAndName(ctx, req.OwnerName, req.RepoName)
	if err != nil {
		return TransactionDecision{}, err
	}
	if repository.ID != scope.RepositoryID {
		return TransactionDecision{Reason: "operation scope mismatch"}, nil
	}
	ownerKind, installationID, operationID := splitOwner(reservation.Owner)
	if ownerKind == model.OwnerOrdinary {
		for _, line := range req.Lines {
			if line.Ref != scope.Ref {
				return TransactionDecision{Reason: "operation scope mismatch"}, nil
			}
		}
		return TransactionDecision{Allowed: true}, nil
	}
	if ownerKind != model.OwnerConditional {
		return TransactionDecision{Reason: "operation scope mismatch"}, nil
	}
	switch req.Phase {
	case PhaseCommitted, PhaseAborted:
		return TransactionDecision{Allowed: true}, nil
	case PhasePrepared:
		return s.admitPrepared(ctx, reservation, scope, repository, installationID, operationID, req.Lines)
	default:
		return TransactionDecision{Reason: "unknown transaction phase"}, nil
	}
}

func splitOwner(owner string) (kind, installationID, operationID string) {
	rest, ok := strings.CutPrefix(owner, "cond:")
	if !ok {
		if strings.HasPrefix(owner, "ord:") {
			return model.OwnerOrdinary, "", ""
		}
		return "", "", ""
	}
	var cut bool
	installationID, operationID, cut = strings.Cut(rest, "/")
	if !cut || installationID == "" || operationID == "" {
		return "", "", ""
	}
	return model.OwnerConditional, installationID, operationID
}

func (s *Service) admitPrepared(ctx context.Context, reservation *model.Reservation, scope Scope, repository *repo_model.Repository, installationID, operationID string, lines []RefLine) (TransactionDecision, error) {
	op, err := model.LookupOperation(ctx, installationID, operationID)
	if err != nil {
		return TransactionDecision{}, err
	}
	if op == nil || !op.Submitted || op.IsTerminal() || op.EffectState != model.EffectPending {
		return TransactionDecision{Reason: "operation is not pending"}, nil
	}
	// The disclosed test barrier pauses here, before any check, so races
	// can be staged deterministically against the atomic decision below.
	if err := testAdmissionBarrier(); err != nil {
		return TransactionDecision{}, err
	}
	reason := ""
	switch {
	case len(lines) != 1:
		reason = model.ReasonUnexpectedRefEffects
	case lines[0].Ref != scope.Ref ||
		!strings.EqualFold(lines[0].Old, scope.OldOID) ||
		!strings.EqualFold(lines[0].New, scope.NewOID):
		reason = model.ReasonStaleBaseOrResult
	default:
		head, err := s.readRef(ctx, repository.RepoPath(), scope.HeadRef)
		if err != nil {
			return TransactionDecision{}, err
		}
		if !strings.EqualFold(head, scope.HeadOID) {
			reason = model.ReasonStaleHead
		}
	}
	if reason == "" {
		// Retargeting the PR after preparation invalidates the request
		// even if an object ID happens to match.
		pr, err := issues_model.GetPullRequestByIndex(ctx, scope.RepositoryID, scope.PRNumber)
		if err != nil {
			return TransactionDecision{}, err
		}
		if pr.HasMerged || pr.HeadRepoID != scope.RepositoryID ||
			pr.HeadBranch != strings.TrimPrefix(scope.HeadRef, git.BranchPrefix) ||
			pr.BaseBranch != strings.TrimPrefix(scope.Ref, git.BranchPrefix) ||
			pr.BaseRepoID != scope.RepositoryID {
			reason = model.ReasonPRMismatch
		}
	}
	if reason == "" {
		if err := s.revalidateSubmission(ctx, op); err != nil {
			if errors.Is(err, ErrAuthorityLost) {
				reason = model.ReasonAuthorityLost
			} else {
				return TransactionDecision{}, err
			}
		}
	}
	if reason == "" && s.clock() >= op.NotAfter {
		reason = model.ReasonExpiredBeforeAdmission
	}
	recorded, admitted, err := model.RecordAdmissionAttempt(ctx, installationID, operationID, reservation.Owner, reason == "", reason)
	if err != nil {
		if errors.Is(err, model.ErrWrongOwner) {
			return TransactionDecision{Reason: model.ReasonWrongOwner}, nil
		}
		if errors.Is(err, model.ErrAdmissionLost) {
			return TransactionDecision{Reason: "operation is not pending"}, nil
		}
		return TransactionDecision{}, err
	}
	if !admitted {
		return TransactionDecision{Reason: lostAdmissionReason(recorded)}, nil
	}
	return TransactionDecision{Allowed: true}, nil
}

func lostAdmissionReason(op *model.Operation) string {
	if op == nil {
		return model.ReasonWrongOwner
	}
	if op.Admitted {
		return model.ReasonDuplicateAdmission
	}
	if op.Revoked {
		return model.ReasonCancelledBeforeAdmission
	}
	if op.Reason != "" {
		return op.Reason
	}
	return model.ReasonWrongOwner
}

// ClassifyCompletion binds synchronous post-receive bookkeeping to the held
// owner. An idle reservation keeps existing behavior. A held reservation
// requires the owner's execution proof and effects within its scope, and
// refuses push-option policy changes under operation ownership.
func (s *Service) ClassifyCompletion(ctx context.Context, repositoryID int64, refNames []string, proof string, hasPushOptions bool) (TransactionDecision, error) {
	reservation, err := model.ReadReservation(ctx)
	if err != nil {
		return TransactionDecision{}, err
	}
	if reservation.Owner == "" {
		return TransactionDecision{Allowed: true}, nil
	}
	if !execcontext.VerifyProof(reservation.Verifier, proof) {
		return TransactionDecision{Reason: "native operation in progress"}, nil
	}
	if hasPushOptions {
		return TransactionDecision{Reason: "push options are not permitted under operation ownership"}, nil
	}
	scope, err := decodeScope(reservation.Scope)
	if err != nil {
		return TransactionDecision{}, err
	}
	if repositoryID != scope.RepositoryID {
		return TransactionDecision{Reason: "operation scope mismatch"}, nil
	}
	for _, ref := range refNames {
		if ref != scope.Ref {
			return TransactionDecision{Reason: "operation scope mismatch"}, nil
		}
	}
	return TransactionDecision{Allowed: true}, nil
}

// revalidateSubmission reloads the bound credential generation, binding and
// current native authority from authoritative state without the submitted
// secret, which was discarded after verification. It mirrors the submission
// verifier's tail checks; any failure is ErrAuthorityLost.
func (s *Service) revalidateSubmission(ctx context.Context, op *model.Operation) error {
	lost := func() error { return ErrAuthorityLost }
	token := new(auth_model.AccessToken)
	has, err := db.GetEngine(ctx).ID(op.TokenID).NoAutoCondition().Get(token)
	if err != nil {
		return err
	}
	if !has {
		return lost()
	}
	if extensionauth.CredentialFingerprint(token.TokenHash, token.TokenSalt) != op.CredentialFingerprint {
		return lost()
	}
	if token.UID != op.ActorID {
		return lost()
	}
	binding, err := extensionauth.FindBinding(ctx, op.InstallationID, token.ID, op.ActorID, op.RepositoryID, op.Kind)
	if err != nil {
		return err
	}
	if binding == nil {
		return lost()
	}
	granted, err := token.Scope.HasScope(auth_model.AccessTokenScopeWriteRepository)
	if err != nil || !granted {
		return lost()
	}
	if !token.ResourceAllRepos {
		resources, err := auth_model.GetRepositoriesAccessibleWithToken(ctx, token.ID)
		if err != nil {
			return err
		}
		allowed := false
		for _, resource := range resources {
			if resource.RepoID == op.RepositoryID {
				allowed = true
				break
			}
		}
		if !allowed {
			return lost()
		}
	}
	user, err := user_model.GetUserByID(ctx, op.ActorID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return lost()
		}
		return err
	}
	if !user.IsActive || user.ProhibitLogin {
		return lost()
	}
	repository, err := repo_model.GetRepositoryByID(ctx, op.RepositoryID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return lost()
		}
		return err
	}
	permission, err := access_model.GetUserRepoPermission(ctx, repository, user)
	if err != nil {
		return err
	}
	if !permission.CanWrite(unit.TypeCode) {
		return lost()
	}
	return nil
}

// testAdmissionBarrier pauses before the atomic admission decision when the
// disclosed test instrument NATIVEOP_TEST_ADMISSION_BARRIER names a
// directory: it writes admission.entered and waits for admission.release.
// Production never sets this variable; the bounded pause and the rename
// interposer are test instruments only.
func testAdmissionBarrier() error {
	dir := os.Getenv("NATIVEOP_TEST_ADMISSION_BARRIER")
	if dir == "" {
		return nil
	}
	if err := os.WriteFile(filepath.Join(dir, "admission.entered"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return err
	}
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "admission.release")); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("admission barrier timeout")
}
