// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
)

// Recovery verdicts for one offline reconciliation.
const (
	// RecoveryReleased means the known effect was established and the
	// exact owner released.
	RecoveryReleased = "released"
	// RecoveryFenced means the owner stays held: wrong owner/generation,
	// unaccounted effects or uncertain attribution.
	RecoveryFenced = "fenced"
	// RecoveryIdle means the reservation already holds no owner.
	RecoveryIdle = "idle"
)

// Bounded recovery refusal reasons. Checks carry the detail; these codes
// stay stable for operators and proof drivers.
const (
	ReasonRecoveryWrongGeneration = "wrong_generation"
	ReasonRecoveryUnknownFamily   = "unknown_family"
	ReasonRecoveryUncertainEffect = "uncertain_effect"
	ReasonRecoveryUnaccounted     = "unaccounted_effects"
	ReasonRecoveryMissingEvidence = "missing_evidence"
)

// RecoveryAssessment is the reconciled verdict for one held owner. It
// carries evidence summaries only; verifiers, secrets and fingerprints
// never appear.
type RecoveryAssessment struct {
	Owner      string
	Generation int64
	OwnerKind  string
	Family     string
	Verdict    string
	Effect     string
	Reason     string
	Checks     []string
}

// ErrNotInhibited refuses offline recovery while the writer domain is not
// inhibited. Stop every native writer and inhibit restart through the
// deployment's controls before reconciling.
var ErrNotInhibited = errors.New("native mutation domain is not inhibited for offline recovery")

// Recover reconciles one held owner offline and releases it only when its
// exact owner/generation matches and its effect is known from authoritative
// evidence. Wrong owners, stale generations, unaccounted ordinary effects
// and uncertain attribution stay fenced. There is no force unlock, no
// timeout release and no replay of the native effect.
func (s *Service) Recover(ctx context.Context, owner string, generation int64) (RecoveryAssessment, error) {
	if !model.OfflineInhibited() {
		return RecoveryAssessment{}, ErrNotInhibited
	}
	reservation, err := model.ReadReservation(ctx)
	if err != nil {
		return RecoveryAssessment{}, err
	}
	if reservation.Owner == "" {
		return RecoveryAssessment{Verdict: RecoveryIdle, Checks: []string{"reservation is idle; nothing to recover"}}, nil
	}
	assessment := RecoveryAssessment{
		Owner:      reservation.Owner,
		Generation: reservation.Generation,
		OwnerKind:  reservation.OwnerKind,
		Verdict:    RecoveryFenced,
	}
	if owner != reservation.Owner {
		assessment.Reason = model.ReasonWrongOwner
		assessment.Checks = []string{"named owner does not hold the reservation"}
		return assessment, nil
	}
	if generation != reservation.Generation {
		assessment.Reason = ReasonRecoveryWrongGeneration
		assessment.Checks = []string{fmt.Sprintf("named generation %d does not match held generation %d", generation, reservation.Generation)}
		return assessment, nil
	}
	scope, err := decodeScope(reservation.Scope)
	if err != nil {
		assessment.Reason = ReasonRecoveryUnknownFamily
		assessment.Checks = []string{"held scope is undecodable; effect is unclassified"}
		return assessment, nil
	}
	assessment.Family = scope.Family
	kind, installationID, operationID := splitOwner(reservation.Owner)
	switch {
	case kind == model.OwnerConditional:
		return s.recoverConditional(ctx, &assessment, reservation, scope, installationID, operationID)
	case kind == model.OwnerOrdinary:
		return s.recoverOrdinary(ctx, &assessment, reservation, scope)
	default:
		assessment.Reason = ReasonRecoveryUnknownFamily
		assessment.Checks = []string{"owner names no known conditional or ordinary holder"}
		return assessment, nil
	}
}

func fenced(assessment *RecoveryAssessment, reason, check string) (RecoveryAssessment, error) {
	assessment.Verdict = RecoveryFenced
	assessment.Reason = reason
	assessment.Checks = append(assessment.Checks, check)
	return *assessment, nil
}

// recoverConditional reconciles one held conditional merge. An attributable
// distinct expected target under the intact reservation establishes
// committed; a proven pre-admission refusal with no remaining writer
// establishes not_committed. Anything else stays indeterminate and fenced.
func (s *Service) recoverConditional(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope, installationID, operationID string) (RecoveryAssessment, error) {
	assessment.Family = "conditional-merge"
	op, err := model.LookupOperation(ctx, installationID, operationID)
	if err != nil {
		return RecoveryAssessment{}, err
	}
	if op == nil || !op.Submitted {
		return fenced(assessment, ReasonRecoveryMissingEvidence, "operation record is missing for the held owner")
	}
	if op.Kind != model.KindMerge {
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("operation kind %q has no offline reconciliation yet", op.Kind))
	}
	repository, err := repo_model.GetRepositoryByID(ctx, scope.RepositoryID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return fenced(assessment, ReasonRecoveryMissingEvidence, "repository for the held scope no longer exists")
		}
		return RecoveryAssessment{}, err
	}
	tip, absent, fence, err := s.recoveryTip(ctx, assessment, repository.RepoPath(), scope.Ref)
	if err != nil || fence {
		return *assessment, err
	}
	pr, err := issues_model.GetPullRequestByIndex(ctx, scope.RepositoryID, scope.PRNumber)
	if err != nil {
		return fenced(assessment, ReasonRecoveryMissingEvidence, "pull request for the held scope cannot be read")
	}
	mergedAtTip := pr.HasMerged && !absent && strings.EqualFold(pr.MergedCommitID, tip)
	assessment.Checks = append(assessment.Checks,
		fmt.Sprintf("operation admitted=%t revoked=%t effect=%s", op.Admitted, op.Revoked, op.EffectState),
		fmt.Sprintf("base tip absent=%t matches_new=%t matches_old=%t pr_merged_at_tip=%t", absent, !absent && strings.EqualFold(tip, scope.NewOID), !absent && strings.EqualFold(tip, scope.OldOID), mergedAtTip),
	)
	committed := op.Admitted && !absent && strings.EqualFold(tip, scope.NewOID) && !strings.EqualFold(tip, scope.OldOID) && mergedAtTip
	notCommitted := !op.Admitted && !absent && strings.EqualFold(tip, scope.OldOID) && !pr.HasMerged
	switch {
	case committed:
		if op.IsTerminal() && op.EffectState != model.EffectCommitted {
			return fenced(assessment, ReasonRecoveryUncertainEffect, "terminal record contradicts the observed merge effect; preserved")
		}
		if !op.IsTerminal() {
			receipt, _ := json.Marshal(MergeReceipt{
				OldOID:       scope.OldOID,
				NewOID:       scope.NewOID,
				ActorID:      op.ActorID,
				RepositoryID: op.RepositoryID,
				PRNumber:     scope.PRNumber,
				PRID:         pr.ID,
				HeadRef:      scope.HeadRef,
				BaseRef:      scope.Ref,
				Method:       "fast-forward-only",
			})
			if _, err := model.SetTerminal(ctx, op.InstallationID, op.OperationID, model.TerminalOutcome{
				EffectState:  model.EffectCommitted,
				Cancellation: model.CancellationNone,
				Completion:   model.CompletionComplete,
				Receipt:      string(receipt),
			}, ""); err != nil {
				return RecoveryAssessment{}, err
			}
		}
		return s.releaseRecovered(ctx, assessment, reservation, model.EffectCommitted)
	case notCommitted:
		if op.IsTerminal() && op.EffectState != model.EffectNotCommitted {
			return fenced(assessment, ReasonRecoveryUncertainEffect, "terminal record contradicts the observed refusal; preserved")
		}
		if !op.IsTerminal() {
			reason := op.Reason
			if reason == "" {
				reason = model.ReasonRecoveredNoEffect
			}
			if _, err := model.SetTerminal(ctx, op.InstallationID, op.OperationID, model.TerminalOutcome{
				EffectState:  model.EffectNotCommitted,
				Reason:       reason,
				Cancellation: model.CancellationNone,
			}, ""); err != nil {
				return RecoveryAssessment{}, err
			}
		}
		return s.releaseRecovered(ctx, assessment, reservation, model.EffectNotCommitted)
	default:
		return fenced(assessment, ReasonRecoveryUncertainEffect, "effect cannot be attributed from admission, tip and pull state")
	}
}

// recoverOrdinary reconciles one held ordinary writer with its family's own
// evidence. A merge-tip comparison never releases another family.
func (s *Service) recoverOrdinary(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope) (RecoveryAssessment, error) {
	switch scope.Family {
	case FamilyBranchCreate:
		return s.recoverBranchCreate(ctx, assessment, reservation, scope)
	case FamilyBranchDelete:
		return s.recoverBranchDelete(ctx, assessment, reservation, scope)
	case FamilyActionsTask:
		return s.recoverActionsTask(ctx, assessment, reservation, scope)
	case FamilyActionsRun:
		return s.recoverActionsRun(ctx, assessment, reservation, scope)
	case FamilyPushCompletion:
		return s.recoverPushCompletion(ctx, assessment, reservation, scope)
	case FamilyReceiveHTTP, FamilyReceiveSSH:
		return s.recoverReceive(ctx, assessment, reservation, scope)
	case FamilyRefWrite:
		return s.recoverRefWrite(ctx, assessment, reservation, scope)
	case FamilyRepoLifecycle:
		return s.recoverRepoLifecycle(ctx, assessment, reservation, scope)
	case FamilyRepoSettings:
		return s.recoverRepoSettings(ctx, assessment, reservation, scope)
	case FamilyProtection:
		return s.recoverProtection(ctx, assessment, reservation, scope)
	case FamilyMirrorSync:
		return s.recoverMirrorSync(ctx, assessment, reservation, scope)
	case FamilyRefSync:
		return s.recoverRefSync(ctx, assessment, reservation, scope)
	case FamilyMaintenance:
		return s.recoverMaintenance(ctx, assessment, reservation, scope)
	case FamilyAuthority:
		return s.recoverAuthority(ctx, assessment, reservation, scope)
	case FamilyCollaboration:
		return s.recoverCollaboration(ctx, assessment, reservation, scope)
	default:
		return fenced(assessment, ReasonRecoveryUnknownFamily, fmt.Sprintf("writer family %q has no offline reconciliation", scope.Family))
	}
}

// recoveryTip reads one ref tip for reconciliation. A missing ref reports
// absent; an unreadable repository fences with missing evidence.
func (s *Service) recoveryTip(ctx context.Context, assessment *RecoveryAssessment, repoPath, ref string) (tip string, absent bool, fence bool, err error) {
	tip, err = s.readRef(ctx, repoPath, ref)
	if err != nil {
		if git.IsErrNotExist(err) {
			return "", true, false, nil
		}
		assessment.Verdict = RecoveryFenced
		assessment.Reason = ReasonRecoveryMissingEvidence
		assessment.Checks = append(assessment.Checks, fmt.Sprintf("ref %q cannot be read", ref))
		return "", false, true, nil
	}
	return strings.ToLower(tip), false, false, nil
}

// releaseRecovered clears the exact held owner after its known effect is
// established and recorded. Terminal operation records and the native
// revision are preserved; stale capability files are retired best-effort.
func (s *Service) releaseRecovered(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, effect string) (RecoveryAssessment, error) {
	if err := model.ReleaseExactOwner(ctx, reservation.Owner, reservation.Generation); err != nil {
		return RecoveryAssessment{}, err
	}
	assessment.Verdict = RecoveryReleased
	assessment.Effect = effect
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("released owner at generation %d with known effect %q", reservation.Generation, effect))
	s.retireStaleCapabilities()
	return *assessment, nil
}

func (s *Service) retireStaleCapabilities() {
	dir, err := s.execDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := os.Remove(dir + "/" + entry.Name()); err != nil {
			log.Warn("recovery: failed to retire stale capability file: %v", err)
		}
	}
}

// recoverBranchCreate reconciles FT03's ordinary branch-create writer: the
// branch ref exists exactly when this owner created it, since no other
// writer could run while it held the reservation.
func (s *Service) recoverBranchCreate(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope) (RecoveryAssessment, error) {
	if scope.RepositoryID <= 0 || scope.Ref == "" {
		return fenced(assessment, ReasonRecoveryUnknownFamily, "branch-create scope names no repository ref")
	}
	repository, err := repo_model.GetRepositoryByID(ctx, scope.RepositoryID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return fenced(assessment, ReasonRecoveryMissingEvidence, "repository for the held scope no longer exists")
		}
		return RecoveryAssessment{}, err
	}
	tip, absent, fence, err := s.recoveryTip(ctx, assessment, repository.RepoPath(), scope.Ref)
	if err != nil || fence {
		return *assessment, err
	}
	_ = tip
	branchName := strings.TrimPrefix(scope.Ref, git.BranchPrefix)
	dbBranch, dbErr := git_model.GetBranch(ctx, scope.RepositoryID, branchName)
	dbMissing := dbErr != nil && git_model.IsErrBranchNotExist(dbErr)
	if dbErr != nil && !dbMissing {
		return RecoveryAssessment{}, dbErr
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("branch ref absent=%t db_missing=%t db_deleted=%t", absent, dbMissing, !dbMissing && dbBranch.IsDeleted))
	switch {
	case !absent:
		return s.releaseRecovered(ctx, assessment, reservation, "created")
	case dbMissing || dbBranch.IsDeleted:
		return s.releaseRecovered(ctx, assessment, reservation, model.EffectNotCommitted)
	default:
		return fenced(assessment, ReasonRecoveryUnaccounted, "branch ref is absent but the database still shows it live")
	}
}

// recoverBranchDelete reconciles the branch-delete writer from its ref and
// database effects together. Either side alone is not attribution: a gone
// ref with a live database row, or a live ref with a deleted row, fences.
func (s *Service) recoverBranchDelete(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope) (RecoveryAssessment, error) {
	if scope.RepositoryID <= 0 || scope.Ref == "" {
		return fenced(assessment, ReasonRecoveryUnknownFamily, "branch-delete scope names no repository ref")
	}
	repository, err := repo_model.GetRepositoryByID(ctx, scope.RepositoryID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return fenced(assessment, ReasonRecoveryMissingEvidence, "repository for the held scope no longer exists")
		}
		return RecoveryAssessment{}, err
	}
	tip, absent, fence, err := s.recoveryTip(ctx, assessment, repository.RepoPath(), scope.Ref)
	if err != nil || fence {
		return *assessment, err
	}
	branchName := strings.TrimPrefix(scope.Ref, git.BranchPrefix)
	dbBranch, dbErr := git_model.GetBranch(ctx, scope.RepositoryID, branchName)
	dbMissing := dbErr != nil && git_model.IsErrBranchNotExist(dbErr)
	if dbErr != nil && !dbMissing {
		return RecoveryAssessment{}, dbErr
	}
	dbDeleted := !dbMissing && dbBranch.IsDeleted
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("branch ref absent=%t matches_deleted_tip=%t db_missing=%t db_deleted=%t",
		absent, !absent && strings.EqualFold(tip, scope.OldOID), dbMissing, dbDeleted))
	switch {
	case absent && (dbDeleted || dbMissing):
		return s.releaseRecovered(ctx, assessment, reservation, "deleted")
	case !absent && !dbDeleted && (dbMissing || strings.EqualFold(tip, scope.OldOID)):
		return s.releaseRecovered(ctx, assessment, reservation, model.EffectNotCommitted)
	default:
		return fenced(assessment, ReasonRecoveryUnaccounted, "branch ref and database deletion marks disagree, or the live tip moved")
	}
}

// recoverActionsTask reconciles one Actions task/job update with its
// resulting commit status. Task, job and status rows must agree; a partial
// update fences for intervention.
func (s *Service) recoverActionsTask(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope) (RecoveryAssessment, error) {
	if scope.TaskID <= 0 {
		return s.recoverActionsTaskPick(ctx, assessment, reservation, scope)
	}
	task, err := actions_model.GetTaskByID(ctx, scope.TaskID)
	if err != nil {
		return fenced(assessment, ReasonRecoveryUnaccounted, "task for the held scope cannot be read")
	}
	job, err := actions_model.GetRunJobByID(ctx, task.JobID)
	if err != nil {
		return fenced(assessment, ReasonRecoveryUnaccounted, "job for the held task cannot be read")
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("task status=%d stopped=%d job status=%d stopped=%d runner_match=%t",
		task.Status, task.Stopped, job.Status, job.Stopped, scope.RunnerID == 0 || task.RunnerID == scope.RunnerID))
	if scope.RunnerID != 0 && task.RunnerID != scope.RunnerID {
		return fenced(assessment, ReasonRecoveryUnaccounted, "task runner does not match the held scope")
	}
	if task.Status.IsDone() {
		if !job.Status.IsDone() || job.Status != task.Status || task.Stopped == 0 || job.Stopped == 0 {
			return fenced(assessment, ReasonRecoveryUnaccounted, "done task and job rows disagree")
		}
		run, err := actions_model.GetRunByID(ctx, job.RunID)
		if err != nil {
			return fenced(assessment, ReasonRecoveryUnaccounted, "run for the held job cannot be read")
		}
		if run.ScheduleID == 0 {
			statuses, _, err := git_model.GetLatestCommitStatus(ctx, job.RepoID, job.CommitSHA, db.ListOptionsAll)
			if err != nil {
				return RecoveryAssessment{}, err
			}
			assessment.Checks = append(assessment.Checks, fmt.Sprintf("commit statuses for job sha: %d", len(statuses)))
			if len(statuses) == 0 {
				return fenced(assessment, ReasonRecoveryUnaccounted, "done job has no resulting commit status")
			}
		}
		return s.releaseRecovered(ctx, assessment, reservation, "updated")
	}
	if job.Status.IsDone() {
		return fenced(assessment, ReasonRecoveryUnaccounted, "job is done while its task is not")
	}
	return s.releaseRecovered(ctx, assessment, reservation, model.EffectNotCommitted)
}

// recoverPushCompletion reconciles one deferred push/completion batch by
// verifying every covered ref still shows the parent push's end state and
// the repository row is intact. No unaccounted writer may have moved a
// covered ref in between. The completion's nested Actions dispatches are
// reconciled too: every unfinished run in the repository must be
// structurally consistent, since only this owner could have created runs
// during the hold and none could have finished. Derived notifications
// and feeds keep their existing best-effort semantics; webhook, mail,
// indexer, mirror and automerge effects are durable queue rows the
// workers retain, and nested schedule rows rebuild idempotently on the
// next push.
func (s *Service) recoverPushCompletion(ctx context.Context, assessment *RecoveryAssessment, reservation *model.Reservation, scope Scope) (RecoveryAssessment, error) {
	if scope.RepositoryID <= 0 || len(scope.Refs) == 0 {
		return fenced(assessment, ReasonRecoveryUnknownFamily, "push-completion scope names no repository refs")
	}
	repository, err := repo_model.GetRepositoryByID(ctx, scope.RepositoryID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return fenced(assessment, ReasonRecoveryMissingEvidence, "repository for the held scope no longer exists")
		}
		return RecoveryAssessment{}, err
	}
	for _, scoped := range scope.Refs {
		tip, absent, fence, err := s.recoveryTip(ctx, assessment, repository.RepoPath(), scoped.Ref)
		if err != nil || fence {
			return *assessment, err
		}
		if isZeroOID(scoped.NewOID) {
			assessment.Checks = append(assessment.Checks, fmt.Sprintf("ref %q deleted, absent=%t", scoped.Ref, absent))
			if !absent {
				return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("ref %q should be deleted", scoped.Ref))
			}
			continue
		}
		match := !absent && strings.EqualFold(tip, scoped.NewOID)
		assessment.Checks = append(assessment.Checks, fmt.Sprintf("ref %q absent=%t matches_expected=%t", scoped.Ref, absent, match))
		if !match {
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("ref %q no longer shows the batch end state", scoped.Ref))
		}
	}
	runs, _, err := db.FindAndCount[actions_model.ActionRun](ctx, actions_model.FindRunOptions{
		RepoID: scope.RepositoryID,
		Status: actions_model.PendingStatuses(),
	})
	if err != nil {
		return RecoveryAssessment{}, err
	}
	for _, run := range runs {
		consistent, detail, err := s.actionsRunConsistent(ctx, run.ID, scope.RepositoryID)
		if err != nil {
			return RecoveryAssessment{}, err
		}
		if !consistent {
			assessment.Checks = append(assessment.Checks, detail)
			return fenced(assessment, ReasonRecoveryUnaccounted, fmt.Sprintf("run %d dispatched under the held push completion is torn", run.ID))
		}
	}
	assessment.Checks = append(assessment.Checks, fmt.Sprintf("nested runs checked=%d", len(runs)))
	return s.releaseRecovered(ctx, assessment, reservation, "consistent")
}

func isZeroOID(oid string) bool {
	if oid == "" {
		return true
	}
	for _, c := range oid {
		if c != '0' {
			return false
		}
	}
	return true
}
