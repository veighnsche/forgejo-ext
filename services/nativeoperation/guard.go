// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	model "forgejo.org/models/nativeoperation"
	execcontext "forgejo.org/modules/nativeoperation"
)

// Ordinary writer families with outer ownership. Each family records its
// affected resource identities in Scope before effects so offline recovery
// can reconcile that family's actual database/ref effects; a merge-tip
// comparison never reconciles another family.
const (
	// FamilyBranchCreate is FT03's ordinary branch-create writer.
	FamilyBranchCreate = "branch"
	// FamilyBranchDelete is the transaction-plus-direct-Git branch delete.
	FamilyBranchDelete = "branch-delete"
	// FamilyActionsTask is one Actions task/job update with its resulting
	// commit status, owned as a single logical update.
	FamilyActionsTask = "actions-task"
	// FamilyPushCompletion is one deferred push/completion batch.
	FamilyPushCompletion = "push-completion"
)

// ScopedRef is one expected ref effect within a multi-ref ordinary scope.
type ScopedRef struct {
	Ref    string `json:"ref"`
	OldOID string `json:"old_oid,omitempty"`
	NewOID string `json:"new_oid,omitempty"`
}

// Scope is the permitted effect recorded on a held reservation. Native hooks
// require the owner's execution proof and effects within this scope; anything
// else refuses.
type Scope struct {
	Kind         string `json:"kind"`
	RepositoryID int64  `json:"repository_id"`
	// Ref is the single permitted branch ref: the merge target for a
	// conditional owner, the created or deleted branch for the ordinary
	// branch writers.
	Ref string `json:"ref,omitempty"`
	// OldOID and NewOID are the exact permitted ref tuple (conditional, or
	// the deleted tip and zero OID for a branch delete).
	OldOID string `json:"old_oid,omitempty"`
	NewOID string `json:"new_oid,omitempty"`
	// HeadRef and HeadOID bind the live source the candidate was verified
	// against (conditional).
	HeadRef string `json:"head_ref,omitempty"`
	HeadOID string `json:"head_oid,omitempty"`
	// PRNumber binds the native PR identity (conditional merge).
	PRNumber int64 `json:"pr_number,omitempty"`
	// Family names the ordinary writer family (ordinary).
	Family string `json:"family,omitempty"`
	// TaskID, JobID, RunID and RunnerID identify one Actions logical
	// update (actions-task). JobID and RunID are resolved from the task
	// when the claim only knows the task.
	TaskID   int64 `json:"task_id,omitempty"`
	JobID    int64 `json:"job_id,omitempty"`
	RunID    int64 `json:"run_id,omitempty"`
	RunnerID int64 `json:"runner_id,omitempty"`
	// Refs lists every expected ref effect of a multi-ref batch
	// (push-completion). Single-ref families use Ref/OldOID/NewOID.
	Refs []ScopedRef `json:"refs,omitempty"`
	// PusherID identifies the pusher whose refs one deferred batch covers.
	PusherID int64 `json:"pusher_id,omitempty"`
	// AuthorityOp names the operation of one ordinary authority writer
	// (user update, member change, key change ...), parsed from its
	// structured resource label. AuthorityID and AuthorityID2 identify
	// the operation's entities. Scopes without an attributable operation
	// (name-based creates, multi-entity batches) stay fenced on recovery.
	AuthorityOp  string `json:"authority_op,omitempty"`
	AuthorityID  int64  `json:"authority_id,omitempty"`
	AuthorityID2 int64  `json:"authority_id2,omitempty"`
}

func conditionalOwner(installationID, operationID string) string {
	return "cond:" + installationID + "/" + operationID
}

func ordinaryOwner(family, resource string) string {
	nonce := make([]byte, 8)
	_, _ = rand.Read(nonce)
	return "ord:" + family + "/" + resource + "/" + hex.EncodeToString(nonce)
}

func encodeScope(scope Scope) (string, error) {
	raw, err := json.Marshal(scope)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeScope(raw string) (Scope, error) {
	var scope Scope
	if raw == "" {
		return scope, errors.New("reservation scope is missing")
	}
	if err := json.Unmarshal([]byte(raw), &scope); err != nil {
		return scope, err
	}
	return scope, nil
}

// WithOrdinaryOwnership claims the idle reservation for one ordinary native
// writer, advancing the revision before its effects, and releases it after
// the writer returns. A busy reservation refuses before any effect. Nested
// calls reuse the enclosing ownership instead of claiming again. A canceled
// context refuses before claiming, and the release runs detached from
// cancellation, so a client disconnect mid-write cannot stick the global
// reservation.
func (s *Service) WithOrdinaryOwnership(ctx context.Context, family, resource string, scope Scope, fn func(ctx context.Context) error) error {
	if execcontext.FromContext(ctx) != nil {
		return fn(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if family == "" || resource == "" {
		return errors.New("ordinary ownership requires a writer identity")
	}
	dir, err := s.execDir()
	if err != nil {
		return err
	}
	path, secret, err := execcontext.WriteCapabilityFile(dir)
	if err != nil {
		return err
	}
	owner := ordinaryOwner(family, resource)
	release := func() {
		_ = os.Remove(path)
	}
	scope.Kind = model.OwnerOrdinary
	scope.Family = family
	encoded, err := encodeScope(scope)
	if err != nil {
		release()
		return err
	}
	claimed, err := model.ClaimOrdinary(ctx, owner, encoded, execcontext.Verifier(secret))
	if err != nil {
		release()
		if errors.Is(err, model.ErrBusy) {
			return fmt.Errorf("%w: ordinary writer %s is fenced", ErrBusy, family)
		}
		if errors.Is(err, model.ErrInhibited) {
			return fmt.Errorf("%w: ordinary writer %s refuses while offline recovery holds the domain", model.ErrInhibited, family)
		}
		return err
	}
	owned := execcontext.NewContext(ctx, &execcontext.Execution{Owner: owner, Generation: claimed.Generation, CapabilityPath: path})
	fnErr := fn(owned)
	if err := model.ReleaseOwner(context.WithoutCancel(ctx), owner); err != nil {
		release()
		if fnErr != nil {
			return fmt.Errorf("%w (and failed to release %s: %v)", fnErr, owner, err)
		}
		return fmt.Errorf("failed to release %s: %w", owner, err)
	}
	release()
	return fnErr
}
