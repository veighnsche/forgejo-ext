// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/modules/setting"
	web_extensions "forgejo.org/routers/web/extensions"
	runtime "forgejo.org/services/extensions"
	"forgejo.org/tests"

	"github.com/coder/websocket"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// The inert-socket mode proves that admitted terminal Reserve reaches Soda's
// host boundary. SODA_R03_HOST_SOCKET selects the real native host path; its
// task-owned p... project container must be running with a soda-tester account.
// This is development evidence, not an installed or release qualification run.
func TestSodaExtensionLiveTerminal(t *testing.T) {
	backend, dashboard, artifactRoot := os.Getenv("SODA_C03_BACKEND"), os.Getenv("SODA_C03_DASHBOARD"), os.Getenv("SODA_C03_ARTIFACT_DIR")
	if backend == "" || dashboard == "" || artifactRoot == "" {
		t.Skip("set SODA_C03_BACKEND, SODA_C03_DASHBOARD, and SODA_C03_ARTIFACT_DIR for the live terminal check")
	}
	defer tests.PrepareTestEnv(t)()
	for _, binary := range []string{backend, dashboard} {
		info, err := os.Stat(binary)
		require.NoError(t, err)
		require.NotZero(t, info.Mode()&0o111)
	}
	require.NoError(t, os.MkdirAll(artifactRoot, 0o700))
	root, err := os.MkdirTemp(artifactRoot, "r03-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
	socket := filepath.Join(root, "s")
	hostSocket := os.Getenv("SODA_R03_HOST_SOCKET")
	native := hostSocket != ""
	if native {
		info, err := os.Stat(hostSocket)
		require.NoError(t, err)
		require.NotZero(t, info.Mode()&os.ModeSocket)
	} else {
		hostSocket = filepath.Join(root, "unused-host.sock")
	}
	projectID := "p" + strings.Repeat("0", 23) + "3"
	if provided := os.Getenv("SODA_R03_PROJECT_ID"); provided != "" {
		projectID = provided
	}
	login := "soda-tester"
	if provided := os.Getenv("SODA_R03_LOGIN"); provided != "" {
		login = provided
	}

	packageDir := filepath.Join(root, "p", "soda")
	require.NoError(t, os.MkdirAll(filepath.Join(packageDir, "assets"), 0o700))
	backendLog := filepath.Join(root, "backend.log")
	run := "#!/bin/sh\nexec " + shellQuoteC03(backend) + " --soda-socket " + shellQuoteC03(socket) + " 2>" + shellQuoteC03(backendLog) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "run"), []byte(run), 0o700))
	manifest := extension.Manifest{Protocol: extension.Protocol, ID: "soda", Name: "Soda", Version: "0.1.0", Executable: "run", Capabilities: []string{extension.CapabilityActorRead, extension.CapabilityRepositoryRead, extension.CapabilityServiceBridge}, Policies: []string{extension.PolicyForgejoUsername}, Panels: []extension.Panel{{ID: "workspace", Title: "Spaces", Entry: "workspace.js"}}}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), encoded, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "assets", "workspace.js"), []byte("export function mount() {}\n"), 0o600))
	manager := runtime.NewManager(filepath.Join(root, "p"))
	serviceCallback := filepath.Join(root, "h")
	require.NoError(t, manager.SetCallbackHandlerFactory(web_extensions.CallbackHandlerForInstance))
	require.NoError(t, manager.SetServiceCallbackEndpoint(serviceCallback, web_extensions.CallbackHandlerForService()))
	require.NoError(t, manager.SetInstanceStopped(web_extensions.RevokeAdmissionsForInstance))
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Soda extension process did not start: %v", err)
	}
	previousManager := runtime.GetManager()
	runtime.SetDefault(manager)
	t.Cleanup(func() { runtime.SetDefault(previousManager); require.NoError(t, manager.Close()) })
	server := httptest.NewTLSServer(testWebRoutes)
	defer server.Close()
	previousURL := setting.AppURL
	setting.AppURL = server.URL + "/"
	defer func() { setting.AppURL = previousURL }()
	startSodaExtensionServiceWithHost(t, root, socket, dashboard, serviceCallback, hostSocket, server.URL)

	// This database is private to this test. The Forgejo fixture's user2 owns
	// repo1 (ID 1); the project ID and login match the task-owned native fixture.
	database, err := sql.Open("sqlite3", filepath.Join(root, "soda.db")+"?_busy_timeout=5000")
	require.NoError(t, err)
	defer database.Close()
	_, err = database.Exec(`INSERT INTO users(id,login,name) VALUES(2,'user2','Fixture')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO projects(id,name,repository_id,owner_id,repository,ip,ready) VALUES(?, 'Terminal fixture',1,2,'user2/repo1','10.89.0.2',1)`, projectID)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO memberships(project_id,user_id,login) VALUES(?,2,?)`, projectID, login)
	require.NoError(t, err)
	owner := loginUser(t, "user2")
	generationFor := func() string {
		response := owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace?session_check=1"), http.StatusNoContent)
		value := response.Header().Get(extension.SessionGenerationHeader)
		require.Len(t, value, 43)
		return value
	}
	generation := generationFor()
	base := server.URL + "/-/extensions/panels/soda/workspace/api/environments/" + projectID
	request := func(method, endpoint string, body any, expected int) map[string]any {
		var payload []byte
		if body != nil {
			payload, err = json.Marshal(body)
			require.NoError(t, err)
		}
		req, requestErr := http.NewRequest(method, base+endpoint, bytes.NewReader(payload))
		require.NoError(t, requestErr)
		req.AddCookie(owner.GetCookie(setting.SessionConfig.CookieName))
		req.Header.Set("Origin", server.URL)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set(extension.SessionGenerationHeader, generation)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response, requestErr := server.Client().Do(req)
		require.NoError(t, requestErr)
		defer response.Body.Close()
		require.Equal(t, expected, response.StatusCode)
		var value map[string]any
		require.NoError(t, json.NewDecoder(response.Body).Decode(&value))
		return value
	}
	// A malformed ID is rejected only after native actor, membership and
	// repository permission have been resolved for this request.
	badID := request(http.MethodGet, "/terminal-sessions/invalid", nil, http.StatusBadRequest)
	require.Equal(t, "invalid_request", badID["error"].(map[string]any)["code"])
	reserve := request(http.MethodPost, "/terminal-sessions", map[string]any{"cols": 80, "rows": 24, "name": "R03 fixture"}, map[bool]int{true: http.StatusCreated, false: http.StatusServiceUnavailable}[native])
	if !native {
		failure, ok := reserve["error"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "terminal_unavailable", failure["code"])
		t.Log("admitted Reserve reached Soda; native host socket was not configured")
		return
	}
	id, ok := reserve["id"].(string)
	require.True(t, ok)
	require.Len(t, id, 32)
	_, err = hex.DecodeString(id)
	require.NoError(t, err)
	dial := func(action, name string) *websocket.Conn {
		headers := http.Header{"Cookie": {owner.GetCookie(setting.SessionConfig.CookieName).String()}, "Origin": {server.URL}}
		ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		conn, _, dialErr := websocket.Dial(ctx, base+"/terminal", &websocket.DialOptions{HTTPClient: server.Client(), HTTPHeader: headers})
		require.NoError(t, dialErr)
		payload := map[string]any{"action": action, "id": id, "expected_user_id": "2", "repository_id": "1", "session_generation": generation, "cols": 80, "rows": 24}
		if action == "create" {
			payload["name"] = name
		}
		body, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		require.NoError(t, conn.Write(ctx, websocket.MessageText, body))
		return conn
	}
	input := func(conn *websocket.Conn, command string) {
		ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		frame, marshalErr := json.Marshal(map[string]string{"type": "input", "data": base64.StdEncoding.EncodeToString([]byte(command + "\r"))})
		require.NoError(t, marshalErr)
		require.NoError(t, conn.Write(ctx, websocket.MessageText, frame))
	}
	output := func(conn *websocket.Conn, marker string) {
		deadline := time.Now().Add(15 * time.Second)
		var observed strings.Builder
		for time.Now().Before(deadline) {
			ctx, done := context.WithDeadline(context.Background(), deadline)
			kind, body, readErr := conn.Read(ctx)
			done()
			require.NoError(t, readErr)
			require.Equal(t, websocket.MessageText, kind)
			var frame struct{ Type, Data string }
			require.NoError(t, json.Unmarshal(body, &frame))
			if frame.Type != "output" {
				continue
			}
			decoded, decodeErr := base64.StdEncoding.DecodeString(frame.Data)
			require.NoError(t, decodeErr)
			observed.Write(decoded)
			if strings.Contains(observed.String(), marker) {
				return
			}
		}
		t.Fatal("native terminal did not return command output")
	}
	waitClosed := func(conn *websocket.Conn) {
		check, done := context.WithTimeout(context.Background(), 21*time.Second)
		defer done()
		for {
			_, _, readErr := conn.Read(check)
			if readErr == nil {
				continue
			}
			require.NotContains(t, readErr.Error(), "deadline exceeded")
			return
		}
	}
	conn := dial("create", "R03 fixture")
	input(conn, `SODA_R03_PROBE=retained; printf 'R03_%s_OK\n' "$SODA_R03_PROBE"`)
	output(conn, "R03_retained_OK")
	conn.CloseNow()
	var terminal map[string]any
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		state := request(http.MethodGet, "/terminal-sessions/"+id, nil, http.StatusOK)
		terminal, ok = state["terminal"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, id, terminal["id"])
		require.Equal(t, true, terminal["ready"])
		if terminal["attached"] == false {
			break
		}
	}
	require.Equal(t, false, terminal["attached"])
	conn = dial("attach", "")
	input(conn, `printf 'R03_PERSIST_%s\n' "$SODA_R03_PROBE"`)
	output(conn, "R03_PERSIST_retained")
	staleCookie := owner.GetCookie(setting.SessionConfig.CookieName).String()
	owner.MakeRequest(t, NewRequest(t, http.MethodPost, "/user/logout"), http.StatusOK)
	waitClosed(conn)
	conn.CloseNow()
	staleHeaders := http.Header{"Cookie": {staleCookie}, "Origin": {server.URL}}
	staleCtx, staleDone := context.WithTimeout(context.Background(), 5*time.Second)
	stale, _, staleErr := websocket.Dial(staleCtx, base+"/terminal", &websocket.DialOptions{HTTPClient: server.Client(), HTTPHeader: staleHeaders})
	staleDone()
	require.Error(t, staleErr)
	require.Nil(t, stale)
	owner = loginUser(t, "user2")
	generation = generationFor()
	conn = dial("attach", "")
	_, err = database.Exec(`DELETE FROM memberships WHERE project_id=? AND user_id=2`, projectID)
	require.NoError(t, err)
	waitClosed(conn)
	conn.CloseNow()
	_, err = database.Exec(`INSERT INTO memberships(project_id,user_id,login) VALUES(?,2,?)`, projectID, login)
	require.NoError(t, err)
	ended := request(http.MethodPost, "/terminal-sessions/"+id, map[string]string{"action": "end"}, http.StatusOK)
	endedTerminal, ok := ended["terminal"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, endedTerminal["ready"])
}
