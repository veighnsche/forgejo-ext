// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/modules/setting"
	"forgejo.org/tests"
	"github.com/stretchr/testify/require"
)

func TestExtensionHTTPBrowserMetadataBoundary(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	installStreamExtension(t)
	server := httptest.NewTLSServer(testWebRoutes)
	defer server.Close()
	previous := setting.AppURL
	setting.AppURL = server.URL + "/"
	defer func() { setting.AppURL = previous }()
	session := loginUser(t, "user2")
	generation := session.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace?session_check=1"), http.StatusNoContent).Header().Get(extension.SessionGenerationHeader)
	require.Len(t, generation, 43)
	request := func() *http.Request {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/-/extensions/pages/pages/global/api/headers", nil)
		require.NoError(t, err)
		req.AddCookie(session.GetCookie(setting.SessionConfig.CookieName))
		req.Header.Set("Origin", server.URL)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set(extension.SessionGenerationHeader, generation)
		req.Header.Set(extension.ContextHeader, `{"actor":{"id":"forged"}}`)
		req.Header.Set(extension.AdmissionHeader, "forged-admission")
		req.Header.Set("X-Forwarded-User", "forged-user")
		return req
	}
	for _, test := range []struct {
		mutate func(*http.Request)
		status int
	}{
		{func(r *http.Request) { r.Header.Del("Origin") }, http.StatusForbidden},
		{func(r *http.Request) { r.Header.Set("Origin", "https://foreign.invalid") }, http.StatusForbidden},
		{func(r *http.Request) { r.Header.Add("Origin", server.URL) }, http.StatusForbidden},
		{func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, http.StatusForbidden},
		{func(r *http.Request) { r.Header.Add(extension.SessionGenerationHeader, generation) }, http.StatusConflict},
		{func(r *http.Request) { r.Header.Set("Authorization", "Bearer private") }, http.StatusUnauthorized},
	} {
		req := request()
		test.mutate(req)
		response, err := server.Client().Do(req)
		require.NoError(t, err)
		require.Equal(t, test.status, response.StatusCode)
		require.NoError(t, response.Body.Close())
	}
	response, err := server.Client().Do(request())
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var headers http.Header
	require.NoError(t, json.NewDecoder(response.Body).Decode(&headers))
	require.Equal(t, []string{generation}, headers.Values(extension.SessionGenerationHeader))
	require.Equal(t, []string{server.URL}, headers.Values("Origin"))
	require.Equal(t, []string{"same-origin"}, headers.Values("Sec-Fetch-Site"))
	for _, name := range []string{"Cookie", "Authorization", "X-Forwarded-User"} {
		require.Empty(t, headers.Values(name))
	}
	require.Len(t, headers.Get(extension.AdmissionHeader), 43)
	require.NotEqual(t, "forged-admission", headers.Get(extension.AdmissionHeader))
	var authority extension.Authority
	require.NoError(t, json.Unmarshal([]byte(headers.Get(extension.ContextHeader)), &authority))
	require.Equal(t, "2", authority.Actor.ID)
	require.Equal(t, generation, authority.SessionGeneration)
}
