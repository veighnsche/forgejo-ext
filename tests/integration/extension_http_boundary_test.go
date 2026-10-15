// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	generation := session.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/native/session"), http.StatusNoContent).Header().Get(extension.SessionGenerationHeader)
	require.Len(t, generation, 43)
	request := func() *http.Request {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/-/native/pages/pages/global/api/headers", nil)
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

func TestExtensionHTTPScopedTransport(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	installStreamExtension(t)
	server := httptest.NewTLSServer(testWebRoutes)
	defer server.Close()
	previous := setting.AppURL
	setting.AppURL = server.URL + "/"
	defer func() { setting.AppURL = previous }()
	session := loginUser(t, "user2")
	generation := session.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/native/session"), http.StatusNoContent).Header().Get(extension.SessionGenerationHeader)
	client := server.Client()
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request := func(suffix string) *http.Response {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/-/native/pages/pages/global/api/"+suffix, nil)
		require.NoError(t, err)
		req.AddCookie(session.GetCookie(setting.SessionConfig.CookieName))
		req.Header.Set("Origin", server.URL)
		req.Header.Set(extension.SessionGenerationHeader, generation)
		response, err := client.Do(req)
		require.NoError(t, err)
		return response
	}
	response := request("transport/path?state=open&item=a%2Fb&return=/a//b")
	require.Equal(t, http.StatusOK, response.StatusCode)
	var forwarded map[string]string
	require.NoError(t, json.NewDecoder(response.Body).Decode(&forwarded))
	require.NoError(t, response.Body.Close())
	require.Equal(t, map[string]string{"path": "/transport/path", "query": "state=open&item=a%2Fb&return=/a//b", "actor_id": "2"}, forwarded)
	for _, suffix := range []string{"%2e%2e/transport/path", "transport/%2fpath", "transport//path"} {
		t.Run(suffix, func(t *testing.T) {
			response := request(suffix)
			defer response.Body.Close()
			require.Equal(t, http.StatusBadRequest, response.StatusCode)
		})
	}
}

func TestExtensionHTTPRequestCancellationRetiresBackend(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	installStreamExtension(t)
	server := httptest.NewTLSServer(testWebRoutes)
	defer server.Close()
	previous := setting.AppURL
	setting.AppURL = server.URL + "/"
	defer func() { setting.AppURL = previous }()
	session := loginUser(t, "user2")
	generation := session.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/native/session"), http.StatusNoContent).Header().Get(extension.SessionGenerationHeader)
	client := server.Client()
	client.Timeout = 5 * time.Second
	request := func(ctx context.Context, suffix string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/-/native/pages/pages/global/api/transport/"+suffix, nil)
		if err != nil {
			return nil, err
		}
		req.AddCookie(session.GetCookie(setting.SessionConfig.CookieName))
		req.Header.Set("Origin", server.URL)
		req.Header.Set(extension.SessionGenerationHeader, generation)
		return client.Do(req)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		response, err := request(ctx, "stalled")
		if response != nil {
			_ = response.Body.Close()
		}
		completed <- err
	}()
	backendState := func(field string) bool {
		response, err := request(context.Background(), "state")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		var state map[string]bool
		return response.StatusCode == http.StatusOK && json.NewDecoder(response.Body).Decode(&state) == nil && state[field]
	}
	// The backend has flushed a partial response; Fountain still buffers it.
	require.Eventually(t, func() bool { return backendState("started") }, 3*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-completed:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("client cancellation did not retire the native request")
	}
	// A fresh admitted callback observes that the stalled backend was canceled.
	require.Eventually(t, func() bool { return backendState("canceled") }, 3*time.Second, 10*time.Millisecond)
}
