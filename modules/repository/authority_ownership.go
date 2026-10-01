// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	model "forgejo.org/models/nativeoperation"
	execcontext "forgejo.org/modules/nativeoperation"
	"forgejo.org/modules/setting"
)

// authorityScope is the reservation scope one collaborator change records.
// It mirrors the service-level Scope fields the authority family uses; the
// modules package cannot import the service that owns Scope (import cycle),
// so the shape is repeated here and decoded generically there.
type authorityScope struct {
	Kind         string `json:"kind"`
	RepositoryID int64  `json:"repository_id"`
	Family       string `json:"family"`
	AuthorityOp  string `json:"authority_op"`
	AuthorityID  int64  `json:"authority_id"`
	AuthorityID2 int64  `json:"authority_id2"`
}

// withAuthorityOwnership claims the idle native reservation for one
// collaborator change before its effects, advancing the revision so an old
// permission observation cannot authorize a competing conditional write. A
// busy reservation or offline inhibition refuses before any effect. Nested
// calls reuse the enclosing ownership instead of claiming again.
//
// This helper claims at the model level with the same owner, scope,
// verifier and capability shape the service wrapper uses: the service
// package depends on this module, so the module cannot call it. Busy and
// inhibited failures are the shared model sentinels, which outer routers
// still map through the service IsBusy check.
func withAuthorityOwnership(ctx context.Context, repoID, userID int64, fn func(ctx context.Context) error) error {
	if execcontext.FromContext(ctx) != nil {
		return fn(ctx)
	}
	resource := fmt.Sprintf("repo/%d/collaborator/%d", repoID, userID)
	dir := filepath.Join(setting.AppDataPath, "nativeop-exec")
	path, secret, err := execcontext.WriteCapabilityFile(dir)
	if err != nil {
		return err
	}
	release := func() {
		_ = os.Remove(path)
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		release()
		return err
	}
	owner := "ord:authority/" + resource + "/" + hex.EncodeToString(nonce)
	encoded, err := json.Marshal(authorityScope{
		Kind:         model.OwnerOrdinary,
		RepositoryID: repoID,
		Family:       "authority",
		AuthorityOp:  "repo/collaborator",
		AuthorityID:  repoID,
		AuthorityID2: userID,
	})
	if err != nil {
		release()
		return err
	}
	claimed, err := model.ClaimOrdinary(ctx, owner, string(encoded), execcontext.Verifier(secret))
	if err != nil {
		release()
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
