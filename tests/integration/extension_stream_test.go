// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	web_extensions "forgejo.org/routers/web/extensions"
	runtime "forgejo.org/services/extensions"
	"forgejo.org/tests"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func installStreamExtension(t *testing.T) *runtime.Manager {
	t.Helper()
	artifactRoot, err := filepath.Abs(filepath.Join("..", "..", ".artifacts"))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(artifactRoot, 0o700))
	root, err := os.MkdirTemp(artifactRoot, "fws-")
	require.NoError(t, err)
	root, err = filepath.Abs(root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	packageDir := filepath.Join(root, "pages")
	require.NoError(t, os.MkdirAll(filepath.Join(packageDir, "assets"), 0700))
	executable, err := os.Executable()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "run"), []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(executable, "'", "'\"'\"'")+"'\n"), 0700))
	manifest := extension.Manifest{
		Protocol:     extension.Protocol,
		ID:           "pages",
		Name:         "Streams",
		Version:      "1",
		Executable:   "run",
		Capabilities: []string{extension.CapabilityActorRead, extension.CapabilityRepositoryRead, extension.CapabilityContributionAuthorize},
		Pages: []extension.Page{
			{ID: "global", Title: "Global", Scope: "global", Entry: "main.js"},
			{ID: "read", Title: "Read", Scope: "repository", Permission: "read", Entry: "main.js"},
		},
	}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), encoded, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "assets", "main.js"), []byte("export function mount() {}"), 0600))
	manager := runtime.NewManager(root)
	require.NoError(t, manager.SetCallbackHandlerFactory(web_extensions.CallbackHandlerForInstance))
	require.NoError(t, manager.SetInstanceStopped(web_extensions.RevokeAdmissionsForInstance))
	require.NoError(t, manager.Start(context.Background()))
	previous := runtime.GetManager()
	runtime.SetDefault(manager)
	t.Cleanup(func() { runtime.SetDefault(previous); require.NoError(t, manager.Close()) })
	return manager
}

func TestExtensionWebSocketNativeBoundary(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	manager := installStreamExtension(t)
	server := httptest.NewTLSServer(testWebRoutes)
	defer server.Close()
	oldURL := setting.AppURL
	setting.AppURL = server.URL + "/"
	defer func() { setting.AppURL = oldURL }()
	session := loginUser(t, "user2")
	cookie := session.GetCookie(setting.SessionConfig.CookieName)
	require.NotNil(t, cookie)
	path := "/-/extensions/pages/pages/global/api/stream"
	dial := func(headers http.Header) (*websocket.Conn, *http.Response, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return websocket.Dial(ctx, server.URL+path, &websocket.DialOptions{HTTPClient: server.Client(), HTTPHeader: headers})
	}
	headers := http.Header{"Cookie": {cookie.String()}, "Origin": {server.URL}, extension.ContextHeader: {`{"Actor":{"ID":"forged"}}`}, extension.AdmissionHeader: {"forged"}}
	for _, origin := range []string{"", "https://foreign.invalid"} {
		invalid := headers.Clone()
		invalid.Set("Origin", origin)
		conn, _, err := dial(invalid)
		require.Error(t, err)
		require.Nil(t, conn)
	}
	path = "/-/extensions/pages/pages/global/api/redirect-stream"
	redirected, _, redirectErr := dial(headers)
	require.Error(t, redirectErr)
	require.Nil(t, redirected)
	path = "/-/extensions/pages/pages/global/api/stream"
	oversized, _, err := dial(headers)
	require.NoError(t, err)
	limitCtx, limitDone := context.WithTimeout(context.Background(), 5*time.Second)
	_ = oversized.Write(limitCtx, websocket.MessageText, []byte(strings.Repeat("x", 32769)))
	_, _, err = oversized.Read(limitCtx)
	limitDone()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "deadline exceeded")
	_ = oversized.CloseNow()
	conn, _, err := dial(headers)
	require.NoError(t, err)
	defer conn.CloseNow()
	exchange := func(conn *websocket.Conn, text string) {
		check, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, conn.Write(check, websocket.MessageText, []byte(text)))
		kind, body, err := conn.Read(check)
		require.NoError(t, err)
		require.Equal(t, websocket.MessageText, kind)
		require.Equal(t, text, string(body))
	}
	exchange(conn, "admitted authority replaced forged context")
	session.MakeRequest(t, NewRequest(t, http.MethodPost, "/user/logout"), http.StatusOK)
	check, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, _, err = conn.Read(check)
	cancel()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "deadline exceeded")
	stale, _, err := dial(headers)
	require.Error(t, err)
	require.Nil(t, stale)
	session = loginUser(t, "user2")
	headers.Set("Cookie", session.GetCookie(setting.SessionConfig.CookieName).String())
	conn, _, err = dial(headers)
	require.NoError(t, err)
	defer conn.CloseNow()
	exchange(conn, "new native generation")
	require.NoError(t, manager.Close())
	check, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	_, _, err = conn.Read(check)
	cancel()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "deadline exceeded")
}

func TestExtensionWebSocketAuthorityAndLongLifetime(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	installStreamExtension(t)
	server := httptest.NewTLSServer(testWebRoutes)
	defer server.Close()
	oldURL := setting.AppURL
	setting.AppURL = server.URL + "/"
	defer func() { setting.AppURL = oldURL }()
	session := loginUser(t, "user2")
	headers := http.Header{"Cookie": {session.GetCookie(setting.SessionConfig.CookieName).String()}, "Origin": {server.URL}}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, server.URL+"/-/extensions/pages/pages/global/api/stream", &websocket.DialOptions{HTTPClient: server.Client(), HTTPHeader: headers})
	require.NoError(t, err)
	defer conn.CloseNow()
	reader := loginUser(t, "user4")
	readerHeaders := http.Header{"Cookie": {reader.GetCookie(setting.SessionConfig.CookieName).String()}, "Origin": {server.URL}}
	repoConn, _, err := websocket.Dial(ctx, server.URL+"/user2/repo1/extensions/pages/read/api/stream", &websocket.DialOptions{HTTPClient: server.Client(), HTTPHeader: readerHeaders})
	require.NoError(t, err)
	defer repoConn.CloseNow()
	_, err = db.GetEngine(db.DefaultContext).ID(1).Cols("is_private").Update(&repo_model.Repository{IsPrivate: true})
	require.NoError(t, err)
	repositoryRevoked := make(chan error, 1)
	go func() {
		check, done := context.WithTimeout(ctx, 21*time.Second)
		defer done()
		_, _, err := repoConn.Read(check)
		repositoryRevoked <- err
	}()
	// Read stays active to service ping/pong while a quiet terminal exceeds the
	// ordinary HTTP response's 30-second deadline.
	received := make(chan error, 1)
	go func() {
		_, body, err := conn.Read(ctx)
		if err == nil && string(body) != "still live" {
			err = context.Canceled
		}
		received <- err
	}()
	timer := time.NewTimer(31 * time.Second)
	defer timer.Stop()
	select {
	case err := <-received:
		t.Fatalf("stream closed before HTTP deadline: %v", err)
	case <-timer.C:
	}
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte("still live")))
	require.NoError(t, <-received)
	repositoryErr := <-repositoryRevoked
	require.Error(t, repositoryErr)
	require.NotContains(t, repositoryErr.Error(), "deadline exceeded")
	_, err = db.GetEngine(db.DefaultContext).ID(2).Cols("is_restricted").Update(&user_model.User{IsRestricted: true})
	require.NoError(t, err)
	check, done := context.WithTimeout(ctx, 21*time.Second)
	defer done()
	_, _, err = conn.Read(check)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "deadline exceeded")
}
