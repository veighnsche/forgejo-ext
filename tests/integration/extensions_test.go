// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	extension "forgejo.org/modules/extensions"
	runtime "forgejo.org/services/extensions"
	"forgejo.org/tests"

	"github.com/stretchr/testify/require"
)

func TestExtensionNativeAuthentication(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	for _, path := range []string{"/-/extensions/pages/pages/page", "/user/settings/extensions/pages/page", "/admin/extensions/pages/page", "/user2/repo1/extensions/pages/page", "/-/extensions/workspace", "/-/extensions/panels/pages/status/api/state"} {
		t.Run(path, func(t *testing.T) {
			MakeRequest(t, NewRequest(t, http.MethodGet, path), http.StatusUnauthorized)
			MakeRequest(t, NewRequest(t, http.MethodGet, path).AddBasicAuth("user2"), http.StatusUnauthorized)
		})
	}
	session := loginUser(t, "user2")
	session.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/pages/pages/page"), http.StatusNotFound)
	session.MakeRequest(t, NewRequest(t, http.MethodGet, "/admin/extensions/pages/page"), http.StatusForbidden)
	request := NewRequest(t, http.MethodPost, "/-/extensions/pages/pages/page/api/action")
	request.Header.Set("Origin", "https://foreign.invalid")
	session.MakeRequest(t, request, http.StatusForbidden)
}

// The existing test binary is also the disposable installed SDK extension. This
// branch runs before the integration TestMain, without opening the host database.
func init() {
	if os.Getenv(extension.SocketEnv) == "" {
		return
	}
	if err := extension.Serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authority, err := extension.RequestContext(r)
		if err != nil {
			http.Error(w, "authority", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(authority)
	})); err != nil {
		panic(err)
	}
	os.Exit(0)
}

func TestExtensionNativePermissions(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	root, err := os.MkdirTemp("", "fe-")
	require.NoError(t, err)
	defer os.RemoveAll(root)
	packageDir := filepath.Join(root, "pages")
	require.NoError(t, os.MkdirAll(filepath.Join(packageDir, "assets"), 0o700))
	executable, err := os.Executable()
	require.NoError(t, err)
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "'\n"
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "run"), []byte(script), 0o700))
	manifest := extension.Manifest{Protocol: extension.Protocol, ID: "pages", Name: "Demo", Version: "1", Executable: "run", Pages: []extension.Page{
		{ID: "global", Title: "Global", Scope: "global", Entry: "main.js"},
		{ID: "user", Title: "User", Scope: "user", Permission: "user", Entry: "main.js"},
		{ID: "site-admin", Title: "Site admin", Scope: "admin", Permission: "admin", Entry: "main.js"},
		{ID: "read", Title: "Read", Scope: "repository", Permission: "read", Entry: "main.js"},
		{ID: "write", Title: "Write", Scope: "repository", Permission: "write", Entry: "main.js"},
		{ID: "admin", Title: "Admin", Scope: "repository", Permission: "admin", Entry: "main.js"},
	}}
	manifest.Panels = []extension.Panel{{ID: "status", Title: "Status", Entry: "main.js"}}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), encoded, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "assets", "main.js"), []byte("export function mount() {}"), 0o600))
	manager := runtime.NewManager(root)
	require.NoError(t, manager.Start(context.Background()))
	previous := runtime.GetManager()
	runtime.SetDefault(manager)
	defer func() { runtime.SetDefault(previous); require.NoError(t, manager.Close()) }()
	owner := loginUser(t, "user2")
	reader := loginUser(t, "user4")
	for _, page := range []string{"read", "write", "admin"} {
		owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/"+page+"/api/context"), http.StatusOK)
	}
	reader.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/read/api/context"), http.StatusOK)
	reader.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/write/api/context"), http.StatusNotFound)
	reader.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/admin/api/context"), http.StatusNotFound)
	reader.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo2/extensions/pages/read/api/context"), http.StatusNotFound)
	request := NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/read/api/context")
	request.Header.Set(extension.ContextHeader, `{"actor":{"id":1,"site_admin":true}}`)
	response := reader.MakeRequest(t, request, http.StatusOK)
	var authority extension.RequestAuthority
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &authority))
	require.EqualValues(t, 4, authority.Actor.ID)
	require.False(t, authority.Actor.SiteAdmin)
	require.Equal(t, "read", authority.Repository.Permission)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/pages/pages/global"), http.StatusOK)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/user/settings/extensions/pages/user"), http.StatusOK)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/admin/extensions/pages/site-admin"), http.StatusForbidden)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace"), http.StatusOK)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/panels/pages/status/api/context"), http.StatusOK)
	siteAdmin := loginUser(t, "user1")
	siteAdmin.MakeRequest(t, NewRequest(t, http.MethodGet, "/admin/extensions/pages/site-admin"), http.StatusOK)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/assets/pages/main.js"), http.StatusOK)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/admin"), http.StatusOK)
}
