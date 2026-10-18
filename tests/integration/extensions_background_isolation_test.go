// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"testing"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/modules/setting"
	extensions_service "forgejo.org/services/extensions"
	"forgejo.org/tests"

	"github.com/stretchr/testify/require"
)

// TestBrowserLogoutFencesSessionNotBackgroundAdmission proves the two
// authority boundaries stay independent: after a native browser logout the
// old browser session cookie and its session generation refuse fresh
// browser requests, while a separately scoped background credential issued
// before the logout continues only for its exact admitted installation.
// Rotation-equivalent fencing (instance revocation + reissue) applies at
// the background boundary itself, never through the browser session.
func TestBrowserLogoutFencesSessionNotBackgroundAdmission(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")
	cookie := session.GetCookie(setting.SessionConfig.CookieName)
	require.NotNil(t, cookie)
	generation := session.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/native/session"), http.StatusNoContent).Header().Get(extension.SessionGenerationHeader)
	require.Len(t, generation, 43)

	// Background authority is independent of the browser session: an
	// unstarted manager with no extension processes backs the registry,
	// exactly like the native-operation receive proof.
	manager := extensions_service.NewManager(t.TempDir())
	extensions_service.SetDefault(manager)
	t.Cleanup(func() { extensions_service.SetDefault(nil) })
	const installation = "bgsession-proof-installation"
	const otherInstallation = "bgsession-proof-other"
	before, err := manager.IssueRuntimeAdmission("bgsession-proof", installation, "bgsession-proof-1", []string{extension.CapabilityBackgroundOperations})
	require.NoError(t, err)
	claims, ok := manager.VerifyBackgroundAdmission(before)
	require.True(t, ok)
	require.Equal(t, installation, claims.InstallationID)

	// Native browser logout destroys the session both tokens derive from.
	session.MakeRequest(t, NewRequest(t, http.MethodPost, "/user/logout"), http.StatusOK)

	// The old browser session refuses fresh browser requests, with or
	// without its former generation: the session is gone, so no
	// generation can match it anymore.
	stale := NewRequest(t, http.MethodGet, "/-/native/session")
	stale.AddCookie(cookie)
	MakeRequest(t, stale, http.StatusUnauthorized)
	staleGeneration := NewRequest(t, http.MethodGet, "/-/native/session")
	staleGeneration.AddCookie(cookie)
	staleGeneration.Header.Set(extension.SessionGenerationHeader, generation)
	MakeRequest(t, staleGeneration, http.StatusUnauthorized)

	// The background credential survives the browser logout: it is scoped
	// to its exact admitted installation, not to the browser session.
	survived, ok := manager.VerifyBackgroundAdmission(before)
	require.True(t, ok, "browser logout must not fence background admissions")
	require.Equal(t, installation, survived.InstallationID)

	// A credential never crosses its installation scope: a token issued
	// for another installation verifies only there, and forged bytes
	// verify nowhere.
	other, err := manager.IssueRuntimeAdmission("bgsession-proof", otherInstallation, "bgsession-proof-1", []string{extension.CapabilityBackgroundOperations})
	require.NoError(t, err)
	otherClaims, ok := manager.VerifyBackgroundAdmission(other)
	require.True(t, ok)
	require.Equal(t, otherInstallation, otherClaims.InstallationID)
	require.NotEqual(t, installation, otherClaims.InstallationID)
	_, ok = manager.VerifyBackgroundAdmission("forged-admission-token-that-verifies-nowhere")
	require.False(t, ok)

	// Fencing happens at the background boundary itself: revoking the
	// owning instance invalidates the old credential while a reissue for
	// a fresh instance succeeds.
	manager.RevokeBackgroundForInstance("bgsession-proof-1")
	_, ok = manager.VerifyBackgroundAdmission(before)
	require.False(t, ok, "revoked instance admissions must not verify")
	rotated, err := manager.IssueRuntimeAdmission("bgsession-proof", installation, "bgsession-proof-2", []string{extension.CapabilityBackgroundOperations})
	require.NoError(t, err)
	rotatedClaims, ok := manager.VerifyBackgroundAdmission(rotated)
	require.True(t, ok, "reissued background credential succeeds")
	require.Equal(t, installation, rotatedClaims.InstallationID)
}
