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

// This file carries the pull package's ordinary-ownership primitive for
// the collaboration writer family. The pull package cannot import
// services/nativeoperation (that package drives pull.Merge for
// conditional execution), so the claim protocol is mirrored here from
// leaf packages only, next to the ref-write mirror.
// services/nativeoperation/guard.go is the canonical implementation:
// owner strings, scope JSON and the claim-before-effects order below
// must stay wire-compatible with it, and the compatibility test pins
// the shared format. Resource labels must stay parseable by the
// canonical collaboration parser; the wire test pins them too.

// familyCollaboration names the ordinary collaboration writer family. It
// must equal the service family of the same writers.
const familyCollaboration = "collaboration"

// collabScope mirrors the service Scope JSON fields the collaboration
// writers record. Unknown fields in either direction are ignored by
// encoding/json, so service-side additions never break this projection.
type collabScope struct {
	Kind         string `json:"kind"`
	Family       string `json:"family,omitempty"`
	RepositoryID int64  `json:"repository_id"`
}

// The collaboration resource builders mirror the service builders in
// services/nativeoperation/family_collaboration.go. Keep the shapes
// identical; any drift fences offline recovery.
func CollabPullResource(prID int64, op string) string {
	return fmt.Sprintf("pull/%d/%s", prID, op)
}

func CollabPullCreateResource(issueID int64) string {
	return fmt.Sprintf("pull/new/%d", issueID)
}

func CollabReviewResource(reviewID int64, op string) string {
	return fmt.Sprintf("review/%d/%s", reviewID, op)
}

func CollabReviewSubmitResource(issueID int64) string {
	return fmt.Sprintf("review/new/%d", issueID)
}

func CollabCommentCreateResource(issueID int64) string {
	return fmt.Sprintf("comment/new/%d", issueID)
}

func CollabIssueResource(issueID int64, op string) string {
	return fmt.Sprintf("issue/%d/%s", issueID, op)
}

func CollabBatchResource(op string) string {
	return "batch/" + op
}

// withCollabOwnership claims the idle reservation for one pull-service
// collaboration writer, advancing the revision before its effects, and
// releases it after the writer returns. A busy reservation refuses before
// any effect. Nested calls reuse the enclosing ownership instead of
// claiming again, so writers driven under an existing execution
// (conditional operations, bound hook callbacks, enclosing service
// claims) never claim twice.
func withCollabOwnership(ctx context.Context, resource string, repositoryID int64, fn func(ctx context.Context) error) error {
	if execcontext.FromContext(ctx) != nil {
		return fn(ctx)
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
	owner := "ord:" + familyCollaboration + "/" + resource + "/" + hex.EncodeToString(nonce)
	release := func() {
		_ = os.Remove(path)
	}

	encoded, err := json.Marshal(collabScope{
		Kind:         model.OwnerOrdinary,
		Family:       familyCollaboration,
		RepositoryID: repositoryID,
	})
	if err != nil {
		release()
		return err
	}
	claimed, err := model.ClaimOrdinary(ctx, owner, string(encoded), execcontext.Verifier(secret))
	if err != nil {
		release()
		if errors.Is(err, model.ErrBusy) {
			return fmt.Errorf("%w: ordinary writer %s is fenced", model.ErrBusy, familyCollaboration)
		}
		if errors.Is(err, model.ErrInhibited) {
			return fmt.Errorf("%w: ordinary writer %s refuses while offline recovery holds the domain", model.ErrInhibited, familyCollaboration)
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
