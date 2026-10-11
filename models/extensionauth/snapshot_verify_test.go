// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package extensionauth_test

import (
	"testing"

	sdk "forgejo.org/extension-sdk"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/extensionauth"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func createReadToken(t *testing.T, uid int64) *auth_model.AccessToken {
	t.Helper()
	return createScopedToken(t, uid, auth_model.AccessTokenScopeReadRepository)
}

func createScopedToken(t *testing.T, uid int64, scope auth_model.AccessTokenScope) *auth_model.AccessToken {
	t.Helper()
	token := &auth_model.AccessToken{
		UID:              uid,
		Name:             "snapshot-read-test",
		Scope:            scope,
		ResourceAllRepos: true,
	}
	require.NoError(t, auth_model.NewAccessToken(t.Context(), token))
	require.NotEmpty(t, token.Token)
	return token
}

func snapshotReadRequest(token *auth_model.AccessToken, repositoryID int64) extensionauth.SnapshotReadRequest {
	return extensionauth.SnapshotReadRequest{
		InstallationID: testInstallation,
		TokenSecret:    token.Token,
		ActorID:        token.UID,
		RepositoryID:   repositoryID,
		Families:       []string{sdk.SnapshotFamilyRefs},
	}
}

func TestVerifySnapshotReadAcceptsBoundReadCaller(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	// A read-scoped token with any enrolled kind reads; the read grants no
	// mutation kind.
	readToken := createReadToken(t, testActor)
	enroll(t, readToken, testRepository, extensionauth.KindMerge)
	decision, err := extensionauth.VerifySnapshotRead(t.Context(), snapshotReadRequest(readToken, testRepository))
	require.NoError(t, err)
	require.Equal(t, readToken.ID, decision.TokenID)
	require.Equal(t, int64(testActor), decision.ActorID)
	require.NotEmpty(t, decision.CredentialFingerprint)

	// A write-scoped token (read implied) reads as well.
	writeToken := createWriterToken(t, testActor)
	enroll(t, writeToken, testRepository, extensionauth.KindReviewSubmit)
	_, err = extensionauth.VerifySnapshotRead(t.Context(), snapshotReadRequest(writeToken, testRepository))
	require.NoError(t, err)
}

func TestVerifySnapshotReadRefusesUnboundOrUnauthorizedCaller(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	token := createReadToken(t, testActor)
	enroll(t, token, testRepository, extensionauth.KindMerge)

	request := snapshotReadRequest(token, testRepository)
	request.TokenSecret = "0000000000000000000000000000000000000000"
	code, status := refusalCode(t, mustVerifySnapshotRead(t, request))
	require.Equal(t, extensionauth.RefusalInvalidCredential, code)
	require.Equal(t, 401, status)

	request = snapshotReadRequest(token, testRepository)
	request.ActorID = testActor + 100
	code, _ = refusalCode(t, mustVerifySnapshotRead(t, request))
	require.Equal(t, extensionauth.RefusalActorMismatch, code)

	// No binding for this repository: same token, other repo.
	other := snapshotReadRequest(token, testRepository+1)
	code, _ = refusalCode(t, mustVerifySnapshotRead(t, other))
	require.Equal(t, extensionauth.RefusalBindingMissing, code)

	// Actor without read access to a private repository cannot read it:
	// user4 has no access to user2's private repo2.
	outsider := createReadToken(t, 4)
	_, err := extensionauth.EnrollBinding(t.Context(), testInstallation, outsider.ID, 4, 2, extensionauth.KindMerge)
	require.NoError(t, err)
	code, _ = refusalCode(t, mustVerifySnapshotRead(t, snapshotReadRequest(outsider, 2)))
	require.Equal(t, extensionauth.RefusalPermissionLost, code)

	// A token without repository or issue read scope cannot read.
	narrow := createScopedToken(t, testActor, auth_model.AccessTokenScopeReadUser)
	enroll(t, narrow, testRepository, extensionauth.KindMerge)
	code, _ = refusalCode(t, mustVerifySnapshotRead(t, snapshotReadRequest(narrow, testRepository)))
	require.Equal(t, extensionauth.RefusalInsufficientScope, code)

	// Unknown repository is an invalid read, not a leak.
	gone := snapshotReadRequest(token, 999999)
	code, _ = refusalCode(t, mustVerifySnapshotRead(t, gone))
	require.Equal(t, extensionauth.RefusalBindingMissing, code)
}

func mustVerifySnapshotRead(t *testing.T, request extensionauth.SnapshotReadRequest) error {
	t.Helper()
	_, err := extensionauth.VerifySnapshotRead(t.Context(), request)
	return err
}

func TestPublicOnlyBoundTokenCannotReadPrivateRepository(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	token := createScopedToken(t, testActor, "public-only,read:repository")
	enroll(t, token, 2, extensionauth.KindMerge)
	code, status := refusalCode(t, mustVerifySnapshotRead(t, snapshotReadRequest(token, 2)))
	require.Equal(t, extensionauth.RefusalRepositoryExcluded, code)
	require.Equal(t, 403, status)
}

func TestSnapshotReadRequiresEveryRequestedNativeScope(t *testing.T) {
	for _, tc := range []struct {
		name     string
		scope    auth_model.AccessTokenScope
		families []string
		allowed  bool
	}{
		{"issue read", "read:issue", []string{sdk.SnapshotFamilyIssue, sdk.SnapshotFamilyComments, sdk.SnapshotFamilyDependencies}, true},
		{"repository read", "read:repository", []string{sdk.SnapshotFamilyPull, sdk.SnapshotFamilyReviews, sdk.SnapshotFamilyChecks, sdk.SnapshotFamilyRefs, sdk.SnapshotFamilyAncestry}, true},
		{"issue cannot read refs", "read:issue", []string{sdk.SnapshotFamilyRefs}, false},
		{"repository cannot read issue", "read:repository", []string{sdk.SnapshotFamilyIssue}, false},
		{"mixed issue only", "read:issue", []string{sdk.SnapshotFamilyIssue, sdk.SnapshotFamilyRefs}, false},
		{"mixed repository only", "read:repository", []string{sdk.SnapshotFamilyIssue, sdk.SnapshotFamilyRefs}, false},
		{"mixed both", "read:issue,read:repository", []string{sdk.SnapshotFamilyIssue, sdk.SnapshotFamilyRefs}, true},
		{"mixed writes imply reads", "write:issue,write:repository", []string{sdk.SnapshotFamilyComments, sdk.SnapshotFamilyReviews}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, unittest.PrepareTestDatabase())
			token := createScopedToken(t, testActor, tc.scope)
			enroll(t, token, testRepository, extensionauth.KindMerge)
			request := snapshotReadRequest(token, testRepository)
			request.Families = tc.families
			_, err := extensionauth.VerifySnapshotRead(t.Context(), request)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				code, _ := refusalCode(t, err)
				require.Equal(t, extensionauth.RefusalInsufficientScope, code)
			}
		})
	}
}

func TestSnapshotReadRetainsTokenRepositoryRestrictions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	token := createScopedToken(t, testActor, "public-only,read:issue")
	token.ResourceAllRepos = false
	_, err := db.GetEngine(t.Context()).ID(token.ID).Cols("resource_all_repos").Update(token)
	require.NoError(t, err)
	require.NoError(t, auth_model.InsertAccessTokenResourceRepos(t.Context(), token.ID, []*auth_model.AccessTokenResourceRepo{{TokenID: token.ID, RepoID: 1}}))
	enroll(t, token, testRepository, extensionauth.KindMerge)
	request := snapshotReadRequest(token, testRepository)
	request.Families = []string{sdk.SnapshotFamilyDependencies}
	decision, err := extensionauth.VerifySnapshotRead(t.Context(), request)
	require.NoError(t, err)
	require.True(t, decision.PublicOnly)
	require.True(t, decision.AllowsRepository(1, false))
	require.False(t, decision.AllowsRepository(1, true))
	require.False(t, decision.AllowsRepository(3, false))
}
