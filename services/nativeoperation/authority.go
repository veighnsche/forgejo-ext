// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"strconv"
	"strings"

	execcontext "forgejo.org/modules/nativeoperation"
)

// FamilyAuthority is the ordinary writer family for native authority
// changes: users, credentials/keys/auth sources, collaborators,
// organizations and teams, external-directory synchronization and native
// administration. Every mapped authority writer claims this family before
// its effects, advancing the native revision so an old permission
// observation cannot authorize a competing conditional write.
const FamilyAuthority = "authority"

// CrashPointAuthorityAfterEffects pauses one top-level authority writer
// after its effects commit and before its owner releases. It is a
// disclosed test instrument for the interrupted authority-writer recovery
// proof; production never sets the barrier variables.
const CrashPointAuthorityAfterEffects = "authority-after-effects"

// WithAuthorityOwnership claims the idle reservation for one ordinary
// authority writer and releases it after the writer returns. A busy
// reservation or offline inhibition refuses before any effect. Nested
// calls reuse the enclosing ownership without claiming again and without
// pausing at the crash barrier: only the top-level authority claim can
// hold the barrier point.
func WithAuthorityOwnership(ctx context.Context, resource string, repositoryID int64, fn func(ctx context.Context) error) error {
	if execcontext.FromContext(ctx) != nil {
		return fn(ctx)
	}
	scope := Scope{RepositoryID: repositoryID}
	if op, id1, id2, ok := parseAuthorityResource(resource); ok {
		scope.AuthorityOp, scope.AuthorityID, scope.AuthorityID2 = op, id1, id2
	}
	return Default().WithOrdinaryOwnership(ctx, FamilyAuthority, resource, scope, func(ctx context.Context) error {
		if err := fn(ctx); err != nil {
			return err
		}
		return TestCrashBarrier(CrashPointAuthorityAfterEffects)
	})
}

// parseAuthorityResource splits one structured authority resource label
// ("team/5/member/9") into its operation ("team/member") and entity IDs.
// Name-based create labels and multi-entity batch labels carry no
// attributable operation and report ok=false: their writers still order
// before their effects, but offline recovery leaves them fenced.
func parseAuthorityResource(resource string) (op string, id1, id2 int64, ok bool) {
	segments := strings.Split(resource, "/")
	parts := make([]string, 0, len(segments))
	ids := make([]int64, 0, 2)
	for _, segment := range segments {
		if id, err := strconv.ParseInt(segment, 10, 64); err == nil && id > 0 {
			if len(ids) == 2 {
				return "", 0, 0, false
			}
			ids = append(ids, id)
			continue
		}
		if segment == "" {
			return "", 0, 0, false
		}
		if _, err := strconv.ParseInt(segment, 10, 64); err == nil {
			return "", 0, 0, false
		}
		parts = append(parts, segment)
	}
	if len(parts) == 0 || len(ids) == 0 {
		return "", 0, 0, false
	}
	id1 = ids[0]
	if len(ids) == 2 {
		id2 = ids[1]
	}
	return strings.Join(parts, "/"), id1, id2, true
}
