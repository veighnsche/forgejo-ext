// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"errors"
	"fmt"
	"time"

	nativeoperation_model "forgejo.org/models/nativeoperation"
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
	HeadRef      string `json:"head_ref,omitempty"`
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
	return withCollabOwnershipScope(ctx, resource, collabScope{RepositoryID: repositoryID}, fn)
}

func withCollabOwnershipScope(ctx context.Context, resource string, scope collabScope, fn func(context.Context) error) error {
	scope.Kind = nativeoperation_model.OwnerOrdinary
	scope.Family = familyCollaboration
	encoded, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	return withPullOwnership(ctx, familyCollaboration, resource, string(encoded), fn)
}

// Deferred workers may briefly wait for their parent to release ownership.
// This never lends the parent's execution and never retries actual effects.
// Longer contention returns to the durable queue for its normal retry cycle.
func withCollabOwnershipWait(ctx context.Context, resource string, scope collabScope, fn func(context.Context) error) error {
	deadline := time.Now().Add(time.Second)
	backoff := 25 * time.Millisecond
	for {
		entered := false
		err := withCollabOwnershipScope(ctx, resource, scope, func(owned context.Context) error {
			entered = true
			return fn(owned)
		})
		if entered || !errors.Is(err, nativeoperation_model.ErrBusy) || !time.Now().Before(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(backoff, time.Until(deadline))):
		}
		backoff = min(backoff*2, 250*time.Millisecond)
	}
}
