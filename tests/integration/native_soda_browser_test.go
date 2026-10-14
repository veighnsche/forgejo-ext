// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"context"
	"encoding/json"
	"io/fs"
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

	"github.com/stretchr/testify/require"
)

// Opt-in development join: current native handlers, real manager/SDK admission,
// emitted Soda assets and a synthetic domain peer. The browser driver owns its
// isolated profile; this fixture neither installs services nor uses human data.
func TestNativeSodaBrowserJoin(t *testing.T) {
	root, backend, assets := os.Getenv("NFX_I06_ROOT"), os.Getenv("NFX_I06_BACKEND"), os.Getenv("NFX_I06_ASSETS")
	if root == "" || backend == "" || assets == "" {
		t.Skip("set the NFX_I06 fixture paths for the native browser development join")
	}
	defer tests.PrepareTestEnv(t)()
	packages, err := os.MkdirTemp(root, "b-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(packages)) })
	manifest := extension.Manifest{
		Protocol: extension.Protocol, ID: "soda", Name: "Soda", Version: "development", Executable: "run", PreferredWorkspace: true,
		Capabilities: []string{extension.CapabilityActorRead, extension.CapabilityRepositoryRead, extension.CapabilityContributionAuthorize},
		Pages:        []extension.Page{{ID: "spaces", Route: "workspace", Title: "Workspace", Scope: "global", Entry: "soda-spaces-entry.js", Owner: "workspace", Subroutes: []extension.Subroute{{Pattern: "new", Title: "New project"}, {Pattern: "requests/{request}", Title: "Project request"}}}},
		Panels:       []extension.Panel{{ID: "workspace", Title: "Workspace", Entry: "soda-spaces-entry.js", Owner: "workspace"}},
	}
	if path := os.Getenv("NFX_I06_MANIFEST_FILE"); path != "" {
		manifest, err = extension.LoadManifest(filepath.Dir(path))
		require.NoError(t, err)
	}
	packageDir := filepath.Join(packages, manifest.ID)
	require.NoError(t, os.MkdirAll(packageDir, 0o700))
	require.NoError(t, filepath.WalkDir(assets, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(assets, path)
		if err != nil {
			return err
		}
		target := filepath.Join(packageDir, "assets", relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	}))
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), encoded, 0o600))
	if os.Getenv("NFX_I06_MANIFEST_FILE") == "" {
		run := "#!/bin/sh\nexport NFX_I06_MODEL_URL=" + shellQuoteC03(os.Getenv("NFX_I06_MODEL_URL")) + "\nexec " + shellQuoteC03(backend) + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(packageDir, manifest.Executable), []byte(run), 0o700))
	} else {
		binary, err := os.ReadFile(backend)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(packageDir, manifest.Executable), binary, 0o700))
	}
	manager := runtime.NewManager(packages)
	require.NoError(t, manager.SetCallbackHandlerFactory(web_extensions.CallbackHandlerForInstance))
	require.NoError(t, manager.SetInstanceStopped(web_extensions.RevokeAdmissionsForInstance))
	require.NoError(t, manager.Start(context.Background()))
	previous := runtime.GetManager()
	runtime.SetDefault(manager)
	t.Cleanup(func() { runtime.SetDefault(previous); require.NoError(t, manager.Close()) })
	subURL := setting.AppSubURL
	// The FCGI listener strips this path prefix before normal native routes;
	// an HTTP reverse proxy supplies the equivalent prefix-stripping boundary.
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if subURL != "" {
			req.URL.Path = strings.TrimPrefix(req.URL.Path, subURL)
		}
		testWebRoutes.ServeHTTP(w, req)
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	oldURL := setting.AppURL
	setting.AppURL = server.URL + subURL + "/"
	defer func() { setting.AppURL = oldURL }()
	session := loginUser(t, "user2")
	cookie := session.GetCookie(setting.SessionConfig.CookieName)
	require.NotNil(t, cookie)
	// Only synthetic fixture credentials cross this task-private file boundary.
	ready, err := json.Marshal(map[string]string{"url": server.URL, "appSubURL": subURL, "cookieName": cookie.Name, "cookieValue": cookie.Value})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "ready.json"), ready, 0o600))
	deadline := time.After(150 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("native browser driver did not finish within its development deadline")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(root, "done")); err == nil {
				return
			}
		}
	}
}
