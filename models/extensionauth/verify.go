// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package extensionauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"

	auth_model "forgejo.org/models/auth"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"

	"github.com/google/uuid"
)

// Bounded refusal codes for background submission. They identify the failed
// check without exposing secrets or native internals.
const (
	RefusalInvalidIntent      = "invalid_intent"
	RefusalUnknownKind        = "unknown_kind"
	RefusalInvalidCredential  = "invalid_credential"
	RefusalActorMismatch      = "actor_mismatch"
	RefusalBindingMissing     = "binding_missing"
	RefusalInsufficientScope  = "insufficient_scope"
	RefusalRepositoryExcluded = "repository_excluded"
	RefusalAccountUnavailable = "account_unavailable"
	RefusalPermissionLost     = "permission_lost"
)

// RefusalError denies a background submission with a bounded code and the
// HTTP status the dispatcher must return.
type RefusalError struct {
	Code   string
	Status int
}

func (err *RefusalError) Error() string { return "background submission refused: " + err.Code }

// SubmissionRequest carries the caller-selected submission facts plus the
// host-derived installation identity. TokenSecret is discarded after native
// verification and never recorded or logged.
type SubmissionRequest struct {
	InstallationID string
	TokenSecret    string
	ActorID        int64
	RepositoryID   int64
	Kind           string
}

// SubmissionDecision carries the verified submission provenance. The
// credential fingerprint detects regeneration of the same token row; it is
// neither a Bearer [REDACTED] nor public receipt data.
type SubmissionDecision struct {
	TokenID               int64
	ActorID               int64
	CredentialFingerprint string
}

func refuse(code string, status int) *RefusalError { return &RefusalError{Code: code, Status: status} }

// CredentialFingerprint binds a native credential generation from its
// authoritative hash/salt. Regenerating the token row changes it.
func CredentialFingerprint(tokenHash, tokenSalt string) string {
	sum := sha256.Sum256([]byte("extension-background-credential\x00" + tokenHash + "\x00" + tokenSalt))
	return hex.EncodeToString(sum[:])
}

// VerifySubmission authenticates a background submission against the enrolled
// binding and current native authority. Effective authority is the
// intersection of the binding, the native token scope/resource restrictions
// and the current native account/repository permissions. Manifest capability
// is enforced by the admission layer, not here.
func VerifySubmission(ctx context.Context, request SubmissionRequest) (SubmissionDecision, error) {
	if _, err := uuid.Parse(request.InstallationID); err != nil {
		return SubmissionDecision{}, refuse(RefusalInvalidIntent, http.StatusBadRequest)
	}
	if request.TokenSecret == "" || request.ActorID <= 0 || request.RepositoryID <= 0 {
		return SubmissionDecision{}, refuse(RefusalInvalidIntent, http.StatusBadRequest)
	}
	if !ValidKind(request.Kind) {
		return SubmissionDecision{}, refuse(RefusalUnknownKind, http.StatusBadRequest)
	}
	token, err := auth_model.GetAccessTokenBySHA(ctx, request.TokenSecret)
	if err != nil {
		if ctx.Err() != nil {
			return SubmissionDecision{}, ctx.Err()
		}
		if auth_model.IsErrAccessTokenNotExist(err) || auth_model.IsErrAccessTokenEmpty(err) {
			return SubmissionDecision{}, refuse(RefusalInvalidCredential, http.StatusUnauthorized)
		}
		return SubmissionDecision{}, err
	}
	// Always compare against the current authoritative hash/salt, including
	// after a cached-ID lookup, before capturing the fingerprint.
	expected := auth_model.HashToken(request.TokenSecret, token.TokenSalt)
	if subtle.ConstantTimeCompare([]byte(token.TokenHash), []byte(expected)) != 1 {
		return SubmissionDecision{}, refuse(RefusalInvalidCredential, http.StatusUnauthorized)
	}
	if token.UID != request.ActorID {
		return SubmissionDecision{}, refuse(RefusalActorMismatch, http.StatusForbidden)
	}
	binding, err := FindBinding(ctx, request.InstallationID, token.ID, request.ActorID, request.RepositoryID, request.Kind)
	if err != nil {
		return SubmissionDecision{}, err
	}
	if binding == nil {
		return SubmissionDecision{}, refuse(RefusalBindingMissing, http.StatusForbidden)
	}
	// All four background kinds mutate native collaboration state, so the
	// token must currently grant repository write. Kind-specific native
	// checks stay at each kind's own enforcement boundary.
	granted, err := token.Scope.HasScope(auth_model.AccessTokenScopeWriteRepository)
	if err != nil || !granted {
		return SubmissionDecision{}, refuse(RefusalInsufficientScope, http.StatusForbidden)
	}
	if !token.ResourceAllRepos {
		resources, err := auth_model.GetRepositoriesAccessibleWithToken(ctx, token.ID)
		if err != nil {
			return SubmissionDecision{}, err
		}
		allowed := false
		for _, resource := range resources {
			if resource.RepoID == request.RepositoryID {
				allowed = true
				break
			}
		}
		if !allowed {
			return SubmissionDecision{}, refuse(RefusalRepositoryExcluded, http.StatusForbidden)
		}
	}
	user, err := user_model.GetUserByID(ctx, request.ActorID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return SubmissionDecision{}, refuse(RefusalAccountUnavailable, http.StatusForbidden)
		}
		return SubmissionDecision{}, err
	}
	if !user.IsActive || user.ProhibitLogin {
		return SubmissionDecision{}, refuse(RefusalAccountUnavailable, http.StatusForbidden)
	}
	repository, err := repo_model.GetRepositoryByID(ctx, request.RepositoryID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return SubmissionDecision{}, refuse(RefusalInvalidIntent, http.StatusBadRequest)
		}
		return SubmissionDecision{}, err
	}
	permission, err := access_model.GetUserRepoPermission(ctx, repository, user)
	if err != nil {
		return SubmissionDecision{}, err
	}
	if !permission.CanWrite(unit.TypeCode) {
		return SubmissionDecision{}, refuse(RefusalPermissionLost, http.StatusForbidden)
	}
	decision := SubmissionDecision{
		TokenID:               token.ID,
		ActorID:               request.ActorID,
		CredentialFingerprint: CredentialFingerprint(token.TokenHash, token.TokenSalt),
	}
	return decision, nil
}

// SnapshotReadRequest carries the caller-selected snapshot-read facts plus
// the host-derived installation identity. TokenSecret is discarded after
// native verification and never recorded or logged.
type SnapshotReadRequest struct {
	InstallationID string
	TokenSecret    string
	ActorID        int64
	RepositoryID   int64
}

// VerifySnapshotRead authenticates a background snapshot read against an
// enrolled binding and current native read authority. Effective authority is
// the intersection of the binding, the native token scope/resource
// restrictions and the current native account/repository read permissions.
// Unlike submission, reads require only read scope and read access: a
// read-only actor observes nothing it cannot already see, and the read
// grants no mutation kind.
func VerifySnapshotRead(ctx context.Context, request SnapshotReadRequest) (SubmissionDecision, error) {
	if _, err := uuid.Parse(request.InstallationID); err != nil {
		return SubmissionDecision{}, refuse(RefusalInvalidIntent, http.StatusBadRequest)
	}
	if request.TokenSecret == "" || request.ActorID <= 0 || request.RepositoryID <= 0 {
		return SubmissionDecision{}, refuse(RefusalInvalidIntent, http.StatusBadRequest)
	}
	token, err := auth_model.GetAccessTokenBySHA(ctx, request.TokenSecret)
	if err != nil {
		if ctx.Err() != nil {
			return SubmissionDecision{}, ctx.Err()
		}
		if auth_model.IsErrAccessTokenNotExist(err) || auth_model.IsErrAccessTokenEmpty(err) {
			return SubmissionDecision{}, refuse(RefusalInvalidCredential, http.StatusUnauthorized)
		}
		return SubmissionDecision{}, err
	}
	// Always compare against the current authoritative hash/salt, including
	// after a cached-ID lookup, before capturing the fingerprint.
	expected := auth_model.HashToken(request.TokenSecret, token.TokenSalt)
	if subtle.ConstantTimeCompare([]byte(token.TokenHash), []byte(expected)) != 1 {
		return SubmissionDecision{}, refuse(RefusalInvalidCredential, http.StatusUnauthorized)
	}
	if token.UID != request.ActorID {
		return SubmissionDecision{}, refuse(RefusalActorMismatch, http.StatusForbidden)
	}
	bound, err := HasAnyBinding(ctx, request.InstallationID, token.ID, request.ActorID, request.RepositoryID)
	if err != nil {
		return SubmissionDecision{}, err
	}
	if !bound {
		return SubmissionDecision{}, refuse(RefusalBindingMissing, http.StatusForbidden)
	}
	// Snapshot families span issues and code/refs. Either read scope
	// admits the read; write scopes imply their read scope, and per-family
	// unit permissions gate the records below.
	repository, err := token.Scope.HasScope(auth_model.AccessTokenScopeReadRepository)
	if err != nil {
		return SubmissionDecision{}, refuse(RefusalInsufficientScope, http.StatusForbidden)
	}
	issue, err := token.Scope.HasScope(auth_model.AccessTokenScopeReadIssue)
	if err != nil {
		return SubmissionDecision{}, refuse(RefusalInsufficientScope, http.StatusForbidden)
	}
	if !repository && !issue {
		return SubmissionDecision{}, refuse(RefusalInsufficientScope, http.StatusForbidden)
	}
	if !token.ResourceAllRepos {
		resources, err := auth_model.GetRepositoriesAccessibleWithToken(ctx, token.ID)
		if err != nil {
			return SubmissionDecision{}, err
		}
		allowed := false
		for _, resource := range resources {
			if resource.RepoID == request.RepositoryID {
				allowed = true
				break
			}
		}
		if !allowed {
			return SubmissionDecision{}, refuse(RefusalRepositoryExcluded, http.StatusForbidden)
		}
	}
	user, err := user_model.GetUserByID(ctx, request.ActorID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return SubmissionDecision{}, refuse(RefusalAccountUnavailable, http.StatusForbidden)
		}
		return SubmissionDecision{}, err
	}
	if !user.IsActive || user.ProhibitLogin {
		return SubmissionDecision{}, refuse(RefusalAccountUnavailable, http.StatusForbidden)
	}
	repositoryRecord, err := repo_model.GetRepositoryByID(ctx, request.RepositoryID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return SubmissionDecision{}, refuse(RefusalInvalidIntent, http.StatusBadRequest)
		}
		return SubmissionDecision{}, err
	}
	permission, err := access_model.GetUserRepoPermission(ctx, repositoryRecord, user)
	if err != nil {
		return SubmissionDecision{}, err
	}
	if !permission.CanReadAny(unit.TypeCode, unit.TypeIssues, unit.TypePullRequests) {
		return SubmissionDecision{}, refuse(RefusalPermissionLost, http.StatusForbidden)
	}
	return SubmissionDecision{
		TokenID:               token.ID,
		ActorID:               request.ActorID,
		CredentialFingerprint: CredentialFingerprint(token.TokenHash, token.TokenSalt),
	}, nil
}
