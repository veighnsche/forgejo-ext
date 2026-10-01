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

// Scope is the permitted effect recorded on a held reservation. Native hooks
// require the owner's execution proof and effects within this scope; anything
// else refuses.
type Scope struct {
	Kind         string `json:"kind"`
	RepositoryID int64  `json:"repository_id"`
	// Ref is the single permitted branch ref: the merge target for a
	// conditional owner, the created branch for the ordinary branch writer.
	Ref string `json:"ref,omitempty"`
	// OldOID and NewOID are the exact permitted ref tuple (conditional).
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
// calls reuse the enclosing ownership instead of claiming again.
func (s *Service) WithOrdinaryOwnership(ctx context.Context, family, resource string, scope Scope, fn func(ctx context.Context) error) error {
	if execcontext.FromContext(ctx) != nil {
		return fn(ctx)
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
		return err
	}
	owned := execcontext.NewContext(ctx, &execcontext.Execution{Owner: owner, Generation: claimed.Generation, CapabilityPath: path})
	fnErr := fn(owned)
	if err := model.ReleaseOwner(ctx, owner); err != nil {
		release()
		if fnErr != nil {
			return fmt.Errorf("%w (and failed to release %s: %v)", fnErr, owner, err)
		}
		return fmt.Errorf("failed to release %s: %w", owner, err)
	}
	release()
	return fnErr
}
