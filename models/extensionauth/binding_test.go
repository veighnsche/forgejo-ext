// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package extensionauth_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/extensionauth"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

const (
	testInstallation = "11111111-2222-4333-8444-555555555555"
	testActor        = 2 // user2 owns repo1
	testRepository   = 1 // repo1
)

func createWriterToken(t *testing.T, uid int64) *auth_model.AccessToken {
	t.Helper()
	token := &auth_model.AccessToken{
		UID:              uid,
		Name:             "background-test",
		Scope:            auth_model.AccessTokenScopeWriteRepository,
		ResourceAllRepos: true,
	}
	require.NoError(t, auth_model.NewAccessToken(t.Context(), token))
	require.NotEmpty(t, token.Token)
	return token
}

func enroll(t *testing.T, token *auth_model.AccessToken, repositoryID int64, kind string) {
	t.Helper()
	_, err := extensionauth.EnrollBinding(t.Context(), testInstallation, token.ID, token.UID, repositoryID, kind)
	require.NoError(t, err)
}

func refusalCode(t *testing.T, err error) (string, int) {
	t.Helper()
	require.Error(t, err)
	var refusal *extensionauth.RefusalError
	require.True(t, errors.As(err, &refusal), "expected refusal, got %v", err)
	return refusal.Code, refusal.Status
}

func TestEnrollBinding(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	token := createWriterToken(t, testActor)

	binding, err := extensionauth.EnrollBinding(ctx, testInstallation, token.ID, testActor, testRepository, extensionauth.KindMerge)
	require.NoError(t, err)
	require.Equal(t, testInstallation, binding.InstallationID)

	// Identical enrollment is idempotent.
	same, err := extensionauth.EnrollBinding(ctx, testInstallation, token.ID, testActor, testRepository, extensionauth.KindMerge)
	require.NoError(t, err)
	require.Equal(t, binding.ID, same.ID)

	// Other kinds are separate bindings.
	_, err = extensionauth.EnrollBinding(ctx, testInstallation, token.ID, testActor, testRepository, extensionauth.KindPRCreate)
	require.NoError(t, err)
	listed, err := extensionauth.ListBindings(ctx, testInstallation)
	require.NoError(t, err)
	require.Len(t, listed, 2)

	_, err = extensionauth.EnrollBinding(ctx, testInstallation, token.ID, testActor+1000, testRepository, extensionauth.KindMerge)
	require.ErrorContains(t, err, "does not belong to the actor")
	_, err = extensionauth.EnrollBinding(ctx, testInstallation, 999999, testActor, testRepository, extensionauth.KindMerge)
	require.ErrorContains(t, err, "access token does not exist")
	_, err = extensionauth.EnrollBinding(ctx, testInstallation, token.ID, testActor, testRepository, "pull_request.delete")
	require.ErrorContains(t, err, "invalid actor binding")
	_, err = extensionauth.EnrollBinding(ctx, "not-a-uuid", token.ID, testActor, testRepository, extensionauth.KindMerge)
	require.ErrorContains(t, err, "invalid installation")

	revoked, err := extensionauth.RevokeBinding(ctx, testInstallation, token.ID, testActor, testRepository, extensionauth.KindMerge)
	require.NoError(t, err)
	require.True(t, revoked)
	revoked, err = extensionauth.RevokeBinding(ctx, testInstallation, token.ID, testActor, testRepository, extensionauth.KindMerge)
	require.NoError(t, err)
	require.False(t, revoked)
}

func TestVerifySubmissionAcceptsBoundCaller(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	token := createWriterToken(t, testActor)
	enroll(t, token, testRepository, extensionauth.KindMerge)

	decision, err := extensionauth.VerifySubmission(t.Context(), extensionauth.SubmissionRequest{
		InstallationID: testInstallation,
		TokenSecret:    token.Token,
		ActorID:        testActor,
		RepositoryID:   testRepository,
		Kind:           extensionauth.KindMerge,
	})
	require.NoError(t, err)
	require.Equal(t, token.ID, decision.TokenID)
	require.Equal(t, int64(testActor), decision.ActorID)
	require.NotEmpty(t, decision.CredentialFingerprint)
	require.NotContains(t, decision.CredentialFingerprint, token.Token)

	// A second verification exercises the cached-ID lookup path and must
	// still compare against the current authoritative secret.
	again, err := extensionauth.VerifySubmission(t.Context(), extensionauth.SubmissionRequest{
		InstallationID: testInstallation,
		TokenSecret:    token.Token,
		ActorID:        testActor,
		RepositoryID:   testRepository,
		Kind:           extensionauth.KindMerge,
	})
	require.NoError(t, err)
	require.Equal(t, decision.CredentialFingerprint, again.CredentialFingerprint)
}

func TestVerifySubmissionRejects(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	token := createWriterToken(t, testActor)
	enroll(t, token, testRepository, extensionauth.KindMerge)
	valid := extensionauth.SubmissionRequest{
		InstallationID: testInstallation,
		TokenSecret:    token.Token,
		ActorID:        testActor,
		RepositoryID:   testRepository,
		Kind:           extensionauth.KindMerge,
	}

	request := valid
	request.TokenSecret = "0000000000000000000000000000000000000000"
	code, status := refusalCode(t, expectVerify(ctx, request))
	require.Equal(t, extensionauth.RefusalInvalidCredential, code)
	require.Equal(t, http.StatusUnauthorized, status)

	request = valid
	request.ActorID = 1
	code, status = refusalCode(t, expectVerify(ctx, request))
	require.Equal(t, extensionauth.RefusalActorMismatch, code)
	require.Equal(t, http.StatusForbidden, status)

	request = valid
	request.RepositoryID = 2 // repo2 exists; no binding covers it
	code, _ = refusalCode(t, expectVerify(ctx, request))
	require.Equal(t, extensionauth.RefusalBindingMissing, code)

	request = valid
	request.Kind = extensionauth.KindMerge + "-forged"
	code, status = refusalCode(t, expectVerify(ctx, request))
	require.Equal(t, extensionauth.RefusalUnknownKind, code)
	require.Equal(t, http.StatusBadRequest, status)

	request = valid
	request.InstallationID = "22222222-2222-4333-8444-555555555555"
	code, _ = refusalCode(t, expectVerify(ctx, request))
	require.Equal(t, extensionauth.RefusalBindingMissing, code)

	// Binding withdrawal ends admission without touching the token.
	revoked, err := extensionauth.RevokeBinding(ctx, testInstallation, token.ID, testActor, testRepository, extensionauth.KindMerge)
	require.NoError(t, err)
	require.True(t, revoked)
	code, _ = refusalCode(t, expectVerify(ctx, valid))
	require.Equal(t, extensionauth.RefusalBindingMissing, code)
}

func expectVerify(ctx context.Context, request extensionauth.SubmissionRequest) error {
	_, err := extensionauth.VerifySubmission(ctx, request)
	return err
}

func TestVerifySubmissionEnforcesCurrentAuthority(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	t.Run("scope loss", func(t *testing.T) {
		token := createWriterToken(t, testActor)
		enroll(t, token, testRepository, extensionauth.KindMerge)
		token.Scope = auth_model.AccessTokenScopeReadRepository
		_, updateErr := db.GetEngine(ctx).ID(token.ID).Cols("scope").NoAutoTime().Update(token)
		require.NoError(t, updateErr)
		_, err := extensionauth.VerifySubmission(ctx, extensionauth.SubmissionRequest{
			InstallationID: testInstallation, TokenSecret: token.Token,
			ActorID: testActor, RepositoryID: testRepository, Kind: extensionauth.KindMerge,
		})
		code, _ := refusalCode(t, err)
		require.Equal(t, extensionauth.RefusalInsufficientScope, code)
	})

	t.Run("resource exclusion", func(t *testing.T) {
		token := createWriterToken(t, testActor)
		token.ResourceAllRepos = false
		_, updateErr := db.GetEngine(ctx).ID(token.ID).Cols("resource_all_repos").NoAutoTime().Update(token)
		require.NoError(t, updateErr)
		require.NoError(t, auth_model.InsertAccessTokenResourceRepos(ctx, token.ID, []*auth_model.AccessTokenResourceRepo{{TokenID: token.ID, RepoID: 2}}))
		enroll(t, token, testRepository, extensionauth.KindMerge)
		_, err := extensionauth.VerifySubmission(ctx, extensionauth.SubmissionRequest{
			InstallationID: testInstallation, TokenSecret: token.Token,
			ActorID: testActor, RepositoryID: testRepository, Kind: extensionauth.KindMerge,
		})
		code, _ := refusalCode(t, err)
		require.Equal(t, extensionauth.RefusalRepositoryExcluded, code)
	})

	t.Run("permission loss", func(t *testing.T) {
		outsider := createWriterToken(t, 4) // user4 cannot write repo1
		enroll(t, outsider, testRepository, extensionauth.KindMerge)
		_, err := extensionauth.VerifySubmission(ctx, extensionauth.SubmissionRequest{
			InstallationID: testInstallation, TokenSecret: outsider.Token,
			ActorID: 4, RepositoryID: testRepository, Kind: extensionauth.KindMerge,
		})
		code, _ := refusalCode(t, err)
		require.Equal(t, extensionauth.RefusalPermissionLost, code)
	})

	t.Run("regeneration invalidates old secret", func(t *testing.T) {
		token := createWriterToken(t, testActor)
		enroll(t, token, testRepository, extensionauth.KindMerge)
		before, err := extensionauth.VerifySubmission(ctx, extensionauth.SubmissionRequest{
			InstallationID: testInstallation, TokenSecret: token.Token,
			ActorID: testActor, RepositoryID: testRepository, Kind: extensionauth.KindMerge,
		})
		require.NoError(t, err)
		regenerated, err := auth_model.RegenerateAccessTokenByID(ctx, token.ID, testActor)
		require.NoError(t, err)
		require.NotEqual(t, token.Token, regenerated.Token)
		_, err = extensionauth.VerifySubmission(ctx, extensionauth.SubmissionRequest{
			InstallationID: testInstallation, TokenSecret: token.Token,
			ActorID: testActor, RepositoryID: testRepository, Kind: extensionauth.KindMerge,
		})
		code, status := refusalCode(t, err)
		require.Equal(t, extensionauth.RefusalInvalidCredential, code)
		require.Equal(t, http.StatusUnauthorized, status)
		after, err := extensionauth.VerifySubmission(ctx, extensionauth.SubmissionRequest{
			InstallationID: testInstallation, TokenSecret: regenerated.Token,
			ActorID: testActor, RepositoryID: testRepository, Kind: extensionauth.KindMerge,
		})
		require.NoError(t, err)
		require.NotEqual(t, before.CredentialFingerprint, after.CredentialFingerprint)
	})

	t.Run("deletion revokes", func(t *testing.T) {
		token := createWriterToken(t, testActor)
		enroll(t, token, testRepository, extensionauth.KindMerge)
		require.NoError(t, auth_model.DeleteAccessTokenByID(ctx, token.ID, testActor))
		_, err := extensionauth.VerifySubmission(ctx, extensionauth.SubmissionRequest{
			InstallationID: testInstallation, TokenSecret: token.Token,
			ActorID: testActor, RepositoryID: testRepository, Kind: extensionauth.KindMerge,
		})
		code, _ := refusalCode(t, err)
		require.Equal(t, extensionauth.RefusalInvalidCredential, code)
	})
}
