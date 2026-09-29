// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/modules/setting"
	web_extensions "forgejo.org/routers/web/extensions"
	runtime "forgejo.org/services/extensions"
	"forgejo.org/tests"

	"github.com/stretchr/testify/require"
)

// This development check is opt-in because it runs the current Soda binaries
// from the neighboring checkout. It exercises their real private HTTP listener
// and Forgejo's admitted extension process without installing either service.
func TestSodaExtensionLiveBridge(t *testing.T) {
	backend, dashboard, artifactRoot := os.Getenv("SODA_C03_BACKEND"), os.Getenv("SODA_C03_DASHBOARD"), os.Getenv("SODA_C03_ARTIFACT_DIR")
	if backend == "" || dashboard == "" || artifactRoot == "" {
		t.Skip("set SODA_C03_BACKEND, SODA_C03_DASHBOARD, and SODA_C03_ARTIFACT_DIR for the live development check")
	}
	defer tests.PrepareTestEnv(t)()
	for _, binary := range []string{backend, dashboard} {
		info, err := os.Stat(binary)
		require.NoError(t, err)
		require.NotZero(t, info.Mode()&0o111)
	}
	require.NoError(t, os.MkdirAll(artifactRoot, 0o700))
	root, err := os.MkdirTemp(artifactRoot, "c-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
	socket := filepath.Join(root, "s")

	packageDir := filepath.Join(root, "p", "soda")
	require.NoError(t, os.MkdirAll(filepath.Join(packageDir, "assets"), 0o700))
	backendLog := filepath.Join(root, "backend.log")
	run := "#!/bin/sh\nexec " + shellQuoteC03(backend) + " --soda-socket " + shellQuoteC03(socket) + " 2>" + shellQuoteC03(backendLog) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "run"), []byte(run), 0o700))
	manifest := extension.Manifest{Protocol: extension.Protocol, ID: "soda", Name: "Soda", Version: "0.1.0", Executable: "run", Capabilities: []string{extension.CapabilityActorRead, extension.CapabilityServiceBridge}, Policies: []string{extension.PolicyForgejoUsername},
		Pages:  []extension.Page{{ID: "spaces", Title: "Spaces", Scope: "global", Entry: "spaces.js"}},
		Panels: []extension.Panel{{ID: "workspace", Title: "Spaces", Entry: "workspace.js"}}}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), encoded, 0o600))
	for _, name := range []string{"spaces.js", "workspace.js"} {
		require.NoError(t, os.WriteFile(filepath.Join(packageDir, "assets", name), []byte("export function mount() {}\n"), 0o600))
	}
	manager := runtime.NewManager(filepath.Join(root, "p"))
	serviceCallback := filepath.Join(root, "h")
	require.NoError(t, manager.SetCallbackHandlerFactory(web_extensions.CallbackHandlerForInstance))
	require.NoError(t, manager.SetServiceCallbackEndpoint(serviceCallback, web_extensions.CallbackHandlerForService()))
	if err := manager.Start(context.Background()); err != nil {
		logContents, _ := os.ReadFile(backendLog)
		t.Fatalf("Soda extension process did not start: %v: %s", err, string(logContents))
	}
	previous := runtime.GetManager()
	runtime.SetDefault(manager)
	t.Cleanup(func() { runtime.SetDefault(previous); require.NoError(t, manager.Close()) })
	startSodaExtensionService(t, root, socket, dashboard, serviceCallback)

	owner := loginUser(t, "user2")
	other := loginUser(t, "user4")
	generation := func(session *TestSession) string {
		response := session.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace?session_check=1"), http.StatusNoContent)
		value := response.Header().Get(extension.SessionGenerationHeader)
		require.Len(t, value, 43)
		return value
	}
	ownerGeneration, otherGeneration := generation(owner), generation(other)
	pageBase := "/-/extensions/pages/soda/spaces/api/"
	panelBase := "/-/extensions/panels/soda/workspace/api/"
	origin, err := url.Parse(setting.AppURL)
	require.NoError(t, err)
	requestAt := func(base string, session *TestSession, method, endpoint, generation string, body any, status int) map[string]any {
		var req *RequestWrapper
		if body == nil {
			req = NewRequest(t, method, base+endpoint)
		} else {
			req = NewRequestWithJSON(t, method, base+endpoint, body)
		}
		req.Header.Set(extension.SessionGenerationHeader, generation)
		req.Header.Set("Origin", origin.Scheme+"://"+origin.Host)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		response := session.MakeRequest(t, req, status)
		require.Equal(t, status, response.Code)
		if status != http.StatusOK {
			return nil
		}
		var value map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &value))
		return value
	}
	request := func(session *TestSession, method, endpoint, generation string, body any, status int) map[string]any {
		return requestAt(pageBase, session, method, endpoint, generation, body, status)
	}
	panelRequest := func(session *TestSession, method, endpoint, generation string, body any, status int) map[string]any {
		return requestAt(panelBase, session, method, endpoint, generation, body, status)
	}

	ownerSession := request(owner, http.MethodGet, "session", ownerGeneration, nil, http.StatusOK)
	require.Equal(t, "user2", ownerSession["user"].(map[string]any)["login"])
	require.Equal(t, ownerGeneration, ownerSession["session_generation"])
	require.Equal(t, map[string]any{"display_name": "C03 owner"}, request(owner, http.MethodPatch, "me/preferences", ownerGeneration, map[string]string{"display_name": "C03 owner"}, http.StatusOK))
	require.Equal(t, map[string]any{"display_name": "C03 owner"}, request(owner, http.MethodGet, "me/preferences", ownerGeneration, nil, http.StatusOK))
	foreignOrigin := NewRequestWithJSON(t, http.MethodPatch, pageBase+"me/preferences", map[string]string{"display_name": "foreign"})
	foreignOrigin.Header.Set(extension.SessionGenerationHeader, ownerGeneration)
	foreignOrigin.Header.Set("Origin", "https://foreign.invalid")
	owner.MakeRequest(t, foreignOrigin, http.StatusForbidden)
	require.Equal(t, map[string]any{"display_name": "C03 owner"}, request(owner, http.MethodGet, "me/preferences", ownerGeneration, nil, http.StatusOK))

	forged := NewRequestWithJSON(t, http.MethodPatch, pageBase+"me/preferences", map[string]string{"display_name": "C03 other"})
	forged.Header.Set(extension.SessionGenerationHeader, otherGeneration)
	forged.Header.Set("Origin", origin.Scheme+"://"+origin.Host)
	forged.Header.Set("Sec-Fetch-Site", "same-origin")
	forged.Header.Set(extension.ContextHeader, `{"extension_id":"soda","actor":{"id":"2","username":"user2","site_admin":true},"contribution":{"id":"spaces","kind":"page","scope":"global","action":"get"}}`)
	forged.Header.Set(extension.AdmissionHeader, strings.Repeat("x", 43))
	response := other.MakeRequest(t, forged, http.StatusOK)
	require.JSONEq(t, `{"display_name":"C03 other"}`, response.Body.String())
	require.Equal(t, map[string]any{"display_name": "C03 owner"}, request(owner, http.MethodGet, "me/preferences", ownerGeneration, nil, http.StatusOK))
	require.Equal(t, map[string]any{"display_name": "C03 other"}, request(other, http.MethodGet, "me/preferences", otherGeneration, nil, http.StatusOK))
	request(other, http.MethodGet, "session", ownerGeneration, nil, http.StatusConflict)
	request(other, http.MethodDelete, "me/preferences", otherGeneration, nil, http.StatusNotFound)
	require.Equal(t, map[string]any{"display_name": "C03 other"}, request(other, http.MethodGet, "me/preferences", otherGeneration, nil, http.StatusOK))

	// Even a caller that can reach the private listener cannot supply a valid
	// Forgejo admission or change the action carried by that admission.
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	privateRequest, err := http.NewRequest(http.MethodPatch, "http://soda-extension-service/api/me/preferences", strings.NewReader(`{"display_name":"forged"}`))
	require.NoError(t, err)
	privateRequest.Header.Set("Content-Type", "application/json")
	privateRequest.Header.Set(extension.ContextHeader, `{"extension_id":"soda","instance_id":"forged","session_generation":"forged","contribution":{"id":"spaces","kind":"page","scope":"global","action":"get"},"actor":{"id":"2","username":"user2","site_admin":true}}`)
	privateRequest.Header.Set(extension.AdmissionHeader, strings.Repeat("x", 43))
	privateResponse, err := client.Do(privateRequest)
	require.NoError(t, err)
	require.NoError(t, privateResponse.Body.Close())
	require.Equal(t, http.StatusForbidden, privateResponse.StatusCode)
	require.Equal(t, map[string]any{"display_name": "C03 owner"}, request(owner, http.MethodGet, "me/preferences", ownerGeneration, nil, http.StatusOK))

	panelSession := panelRequest(owner, http.MethodGet, "session", ownerGeneration, nil, http.StatusOK)
	require.Equal(t, "user2", panelSession["user"].(map[string]any)["login"])
	require.Equal(t, ownerGeneration, panelSession["session_generation"])
	require.Equal(t, map[string]any{"display_name": "C03 panel"}, panelRequest(owner, http.MethodPatch, "me/preferences", ownerGeneration, map[string]string{"display_name": "C03 panel"}, http.StatusOK))
	require.Equal(t, map[string]any{"display_name": "C03 panel"}, panelRequest(owner, http.MethodGet, "me/preferences", ownerGeneration, nil, http.StatusOK))
	require.Equal(t, map[string]any{"display_name": "C03 panel"}, request(owner, http.MethodGet, "me/preferences", ownerGeneration, nil, http.StatusOK))
}

func startSodaExtensionService(t *testing.T, root, socket, dashboard, serviceCallback string) {
	startSodaExtensionServiceWithHost(t, root, socket, dashboard, serviceCallback, filepath.Join(root, "unused-host.sock"), "https://forgejo.test")
}

func startSodaExtensionServiceWithHost(t *testing.T, root, socket, dashboard, serviceCallback, hostSocket, forgejoURL string) {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "grant-key"), []byte(base64.StdEncoding.EncodeToString(key)), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "oauth-secret"), []byte("development-only"), 0o600))
	port, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listen := port.Addr().String()
	require.NoError(t, port.Close())
	config := map[string]any{
		"listen": listen, "forgejo_url": forgejoURL, "forgejo_internal_url": "http://127.0.0.1:3000",
		"database": filepath.Join(root, "soda.db"), "host_socket": hostSocket,
		"oauth_client_id": "c03-test", "oauth_secret_file": filepath.Join(root, "oauth-secret"),
		"grant_key_file": filepath.Join(root, "grant-key"), "operator_id": 1,
	}
	encoded, err := json.Marshal(config)
	require.NoError(t, err)
	configPath := filepath.Join(root, "dashboard.json")
	require.NoError(t, os.WriteFile(configPath, encoded, 0o600))
	logFile, err := os.Create(filepath.Join(root, "dashboard.log"))
	require.NoError(t, err)
	command := exec.Command(dashboard, "--config", configPath, "--extension-socket", socket)
	command.Env = append(os.Environ(), extension.ServiceCallbackEnv+"="+serviceCallback)
	command.Stdout, command.Stderr = logFile, logFile
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = logFile.Close()
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		connection, dialErr := net.DialTimeout("unix", socket, 100*time.Millisecond)
		if dialErr == nil {
			require.NoError(t, connection.Close())
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	contents, _ := os.ReadFile(filepath.Join(root, "dashboard.log"))
	t.Fatalf("Soda private listener did not start: %s", string(contents))
}

func shellQuoteC03(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
