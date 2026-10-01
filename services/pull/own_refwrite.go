// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	model "forgejo.org/models/nativeoperation"
	execcontext "forgejo.org/modules/nativeoperation"
	"forgejo.org/modules/setting"
)

// This file carries the pull package's ordinary-ownership primitive. The
// pull package cannot import services/nativeoperation (that package drives
// pull.Merge for conditional execution), so the claim protocol is mirrored
// here from leaf packages only. services/nativeoperation/guard.go is the
// canonical implementation: owner strings, scope JSON and the
// claim-before-effects order below must stay wire-compatible with it, and
// the compatibility test pins the shared format.

// refWriteScope mirrors the service Scope JSON fields the pull ref writers
// record. Unknown fields in either direction are ignored by encoding/json,
// so service-side additions never break this projection.
type refWriteScope struct {
	Kind         string `json:"kind"`
	Family       string `json:"family,omitempty"`
	RepositoryID int64  `json:"repository_id"`
	Ref          string `json:"ref,omitempty"`
	OldOID       string `json:"old_oid,omitempty"`
	NewOID       string `json:"new_oid,omitempty"`
	PRNumber     int64  `json:"pr_number,omitempty"`
}

// familyRefWrite names the direct single-ref native writers. It must equal
// the service family of the same writers.
const familyRefWrite = "ref-write"

// Ref-write kinds carried in the resource string.
const (
	refWritePRRef    = "pr-ref"
	refWriteMerge    = "merge"
	refWritePRUpdate = "pr-update"
)

// refWriteResource builds the owner resource string for one direct ref
// write: repository, kind and affected ref.
func refWriteResource(repoID int64, kind, ref string) string {
	return fmt.Sprintf("%d/%s/%s", repoID, kind, ref)
}

// withRefWriteOwnership claims the idle reservation for one pull ref
// writer, advancing the revision before its effects, and releases it after
// the writer returns. A busy reservation refuses before any effect. Nested
// calls reuse the enclosing ownership instead of claiming again, so writers
// driven under an existing execution (conditional merges, bound hook
// callbacks) never claim twice.
func withRefWriteOwnership(ctx context.Context, resource string, scope refWriteScope, fn func(ctx context.Context) error) error {
	if execcontext.FromContext(ctx) != nil {
		return fn(ctx)
	}
	// A canceled caller cannot hold the domain: claiming would strand
	// the reservation when the release below fails on the same ctx.
	if err := ctx.Err(); err != nil {
		return err
	}
	if resource == "" {
		return errors.New("ordinary ownership requires a writer identity")
	}

	dir := filepath.Join(setting.AppDataPath, "nativeop-exec")
	path, secret, err := execcontext.WriteCapabilityFile(dir)
	if err != nil {
		return err
	}
	nonce := make([]byte, 8)
	_, _ = rand.Read(nonce)
	owner := "ord:" + familyRefWrite + "/" + resource + "/" + hex.EncodeToString(nonce)
	release := func() {
		_ = os.Remove(path)
	}

	scope.Kind = model.OwnerOrdinary
	scope.Family = familyRefWrite
	encoded, err := json.Marshal(scope)
	if err != nil {
		release()
		return err
	}
	claimed, err := model.ClaimOrdinary(ctx, owner, string(encoded), execcontext.Verifier(secret))
	if err != nil {
		release()
		if errors.Is(err, model.ErrBusy) {
			return fmt.Errorf("%w: ordinary writer %s is fenced", model.ErrBusy, familyRefWrite)
		}
		if errors.Is(err, model.ErrInhibited) {
			return fmt.Errorf("%w: ordinary writer %s refuses while offline recovery holds the domain", model.ErrInhibited, familyRefWrite)
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

// recordRefWriteResult records the realized new OID of the calling ref
// writer's single-ref scope after the effect is computed. It requires the
// caller's own held ownership.
func recordRefWriteResult(ctx context.Context, newOID string) error {
	exec := execcontext.FromContext(ctx)
	if exec == nil || exec.Owner == "" {
		return errors.New("ref result recording requires ownership")
	}
	reservation, err := model.ReadReservation(ctx)
	if err != nil {
		return err
	}
	if reservation.Owner != exec.Owner {
		return model.ErrWrongOwner
	}
	var scope refWriteScope
	if err := json.Unmarshal([]byte(reservation.Scope), &scope); err != nil {
		return err
	}
	scope.NewOID = newOID
	encoded, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	return model.UpdateScopeWhere(ctx, exec.Owner, string(encoded))
}
