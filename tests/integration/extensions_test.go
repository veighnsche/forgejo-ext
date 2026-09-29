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
	"strconv"
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
	repo_service "forgejo.org/services/repository"
	"forgejo.org/tests"

	ws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
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

func TestNativeSessionWebSocketThroughLoginAndLogout(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")
	cookie := session.GetCookie(setting.SessionConfig.CookieName)
	require.NotNil(t, cookie)

	server := httptest.NewTLSServer(testWebRoutes)
	defer server.Close()
	config, err := websocket.NewConfig("wss"+strings.TrimPrefix(server.URL, "https")+"/-/extensions/test/session-stream", server.URL)
	require.NoError(t, err)
	config.TlsConfig = server.Client().Transport.(*http.Transport).TLSClientConfig
	config.Header.Set("Cookie", cookie.String())
	conn, err := websocket.DialConfig(config)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, websocket.Message.Send(conn, "before-logout"))
	var echoed string
	require.NoError(t, websocket.Message.Receive(conn, &echoed))
	require.Equal(t, "before-logout", echoed)

	// Logout uses Forgejo's normal CSRF and session destruction path while the
	// upgraded request still holds its original session response lifecycle.
	session.MakeRequest(t, NewRequest(t, http.MethodPost, "/user/logout"), http.StatusOK)
	require.NoError(t, websocket.Message.Send(conn, "after-logout"))
	require.NoError(t, websocket.Message.Receive(conn, &echoed))
	require.Equal(t, "after-logout", echoed)
	require.NoError(t, conn.Close())

	request := NewRequest(t, http.MethodGet, "/-/extensions/test/session-stream")
	request.AddCookie(cookie)
	MakeRequest(t, request, http.StatusUnauthorized)
}

// The existing test binary is also the disposable installed SDK extension. This
// branch runs before the integration TestMain, without opening the host database.
func init() {
	if os.Getenv(extension.SocketEnv) == "" {
		return
	}
	if err := extension.Serve(extension.Application{Policies: testUsernamePolicies(), AuthorizeContribution: func(_ context.Context, request extension.ContributionRequest) (extension.ContributionDecision, error) {
		if request.Contribution.Kind == "panel" && request.Contribution.ID == "status" && request.ActorID == "4" {
			return extension.ContributionDecision{Allowed: false, DenialCode: "test_policy"}, nil
		}
		return extension.ContributionDecision{Allowed: true}, nil
	}, HTTP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authority, err := extension.RequestContext(r)
		if err != nil {
			http.Error(w, "authority", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/headers" {
			if _, err := authority.Native().CurrentActor(r.Context()); err != nil {
				http.Error(w, "native callback", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(r.Header)
			return
		}
		if r.URL.Path == "/redirect-stream" {
			http.Redirect(w, r, "/stream", http.StatusFound)
			return
		}
		if r.URL.Path == "/stream" {
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Sec-WebSocket-Protocol") != "" || r.Header.Get(extension.SessionGenerationHeader) != "" {
				http.Error(w, "browser credential leaked", http.StatusForbidden)
				return
			}
			if _, err := authority.Native().CurrentActor(r.Context()); err != nil {
				http.Error(w, "native callback", http.StatusForbidden)
				return
			}
			conn, err := ws.Accept(w, r, &ws.AcceptOptions{InsecureSkipVerify: true})
			if err != nil {
				return
			}
			defer conn.CloseNow()
			for {
				kind, body, err := conn.Read(r.Context())
				if err != nil {
					return
				}
				if err := conn.Write(r.Context(), kind, body); err != nil {
					return
				}
			}
		}
		if r.URL.Path == "/native" {
			actor, err := authority.Native().CurrentActor(r.Context())
			if err != nil {
				http.Error(w, "native callback", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(actor)
			return
		}
		if r.URL.Path == "/native/repository" {
			repository, err := authority.Native().Repository(r.Context(), r.URL.Query().Get("id"))
			if err != nil {
				http.Error(w, "native callback", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(repository)
			return
		}
		if r.URL.Path == "/native/repositories" {
			limit := 10
			if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
				limit, err = strconv.Atoi(rawLimit)
				if err != nil || limit < 1 || limit > 100 {
					http.Error(w, "invalid limit", http.StatusBadRequest)
					return
				}
			}
			page, err := authority.Native().SearchOwnedRepositories(r.Context(), r.URL.Query().Get("query"), r.URL.Query().Get("cursor"), limit)
			if err != nil {
				http.Error(w, "native callback", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(page)
			return
		}
		if r.URL.Path == "/native/organization-owner" {
			owner, err := authority.Native().OrganizationOwner(r.Context(), r.URL.Query().Get("name"))
			if err != nil {
				http.Error(w, "native callback", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(owner)
			return
		}
		if r.URL.Path == "/native/public-keys" {
			keys, err := authority.Native().PublicSSHKeys(r.Context())
			if err != nil {
				http.Error(w, "native callback", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(keys)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(authority)
	})}); err != nil {
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
	manifest := extension.Manifest{Protocol: extension.Protocol, ID: "pages", Name: "Demo", Version: "1", Executable: "run", Capabilities: []string{
		extension.CapabilityActorRead,
		extension.CapabilityRepositoryRead,
		extension.CapabilityOwnedRepositoriesSearch,
		extension.CapabilityOrganizationOwnership,
		extension.CapabilityPublicKeysRead,
		extension.CapabilityContributionAuthorize,
	}, Pages: []extension.Page{
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
	require.NoError(t, manager.SetCallbackHandlerFactory(web_extensions.CallbackHandlerForInstance))
	require.NoError(t, manager.Start(context.Background()))
	previous := runtime.GetManager()
	runtime.SetDefault(manager)
	defer func() { runtime.SetDefault(previous); require.NoError(t, manager.Close()) }()
	owner := loginUser(t, "user2")
	reader := loginUser(t, "user4")
	repoOwner, err := user_model.GetUserByName(db.DefaultContext, "user2")
	require.NoError(t, err)
	for _, name := range []string{"pagination-01", "pagination-02", "pagination-03"} {
		_, err := repo_service.CreateRepository(db.DefaultContext, repoOwner, repoOwner, repo_service.CreateRepoOptions{Name: name})
		require.NoError(t, err)
	}
	getGeneration := func(session *TestSession) string {
		response := session.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace?session_check=1"), http.StatusNoContent)
		generation := response.Header().Get(extension.SessionGenerationHeader)
		require.Len(t, generation, 43)
		return generation
	}
	ownerGeneration := getGeneration(owner)
	readerGeneration := getGeneration(reader)
	apiRequest := func(session *TestSession, generation, method, path string, status int) *httptest.ResponseRecorder {
		request := NewRequest(t, method, path)
		request.Header.Set(extension.SessionGenerationHeader, generation)
		return session.MakeRequest(t, request, status)
	}
	for _, page := range []string{"read", "write", "admin"} {
		apiRequest(owner, ownerGeneration, http.MethodGet, "/user2/repo1/extensions/pages/"+page+"/api/context", http.StatusOK)
	}
	apiRequest(reader, readerGeneration, http.MethodGet, "/user2/repo1/extensions/pages/read/api/context", http.StatusOK)
	reader.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/write/api/context"), http.StatusNotFound)
	reader.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/admin/api/context"), http.StatusNotFound)
	reader.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo2/extensions/pages/read/api/context"), http.StatusNotFound)
	ownerResponse := apiRequest(owner, ownerGeneration, http.MethodGet, "/user2/repo1/extensions/pages/read/api/context", http.StatusOK)
	var ownerAuthority extension.Authority
	require.NoError(t, json.Unmarshal(ownerResponse.Body.Bytes(), &ownerAuthority))
	require.NotEmpty(t, ownerAuthority.InstanceID)
	require.Len(t, ownerAuthority.SessionGeneration, 43)
	request := NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/read/api/context")
	request.Header.Set(extension.ContextHeader, `{"actor":{"id":1,"site_admin":true}}`)
	request.Header.Set(extension.AdmissionHeader, "browser-forged-admission")
	request.Header.Set(extension.SessionGenerationHeader, readerGeneration)
	response := reader.MakeRequest(t, request, http.StatusOK)
	var authority extension.Authority
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &authority))
	require.Equal(t, "4", authority.Actor.ID)
	require.False(t, authority.Actor.SiteAdmin)
	require.Equal(t, "read", authority.Repository.Permission)
	require.Equal(t, ownerAuthority.InstanceID, authority.InstanceID)
	require.NotEqual(t, ownerAuthority.SessionGeneration, authority.SessionGeneration)
	readerCookie := reader.GetCookie(setting.SessionConfig.CookieName)
	require.NotNil(t, readerCookie)
	require.NotContains(t, response.Body.String(), readerCookie.Value)
	require.NotContains(t, response.Body.String(), "browser-forged-admission")
	callbackResponse := apiRequest(reader, readerGeneration, http.MethodGet, "/user2/repo1/extensions/pages/read/api/native", http.StatusOK)
	var callbackActor extension.Actor
	require.NoError(t, json.Unmarshal(callbackResponse.Body.Bytes(), &callbackActor))
	require.Equal(t, extension.Actor{ID: "4", Username: "user4"}, callbackActor)
	repositoryResponse := apiRequest(reader, readerGeneration, http.MethodGet, "/user2/repo1/extensions/pages/read/api/native/repository?id="+ownerAuthority.Repository.ID, http.StatusOK)
	var repository extension.Repository
	require.NoError(t, json.Unmarshal(repositoryResponse.Body.Bytes(), &repository))
	require.Equal(t, ownerAuthority.Repository.ID, repository.ID)
	require.Equal(t, ownerAuthority.Repository.Owner, repository.Owner)
	require.Equal(t, ownerAuthority.Repository.Name, repository.Name)
	require.Equal(t, "read", repository.Permission)
	repoTwoResponse := apiRequest(owner, ownerGeneration, http.MethodGet, "/user2/repo2/extensions/pages/read/api/context", http.StatusOK)
	var repoTwoAuthority extension.Authority
	require.NoError(t, json.Unmarshal(repoTwoResponse.Body.Bytes(), &repoTwoAuthority))
	apiRequest(reader, readerGeneration, http.MethodGet, "/user2/repo1/extensions/pages/read/api/native/repository?id="+repoTwoAuthority.Repository.ID, http.StatusForbidden)
	apiRequest(reader, ownerGeneration, http.MethodGet, "/user2/repo1/extensions/pages/read/api/context", http.StatusConflict)
	reader.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/read/api/context"), http.StatusConflict)
	repositoryModel, err := repo_model.GetRepositoryByOwnerAndName(db.DefaultContext, "user2", "repo1")
	require.NoError(t, err)
	repositoryModel.IsPrivate = true
	require.NoError(t, repo_model.UpdateRepositoryCols(db.DefaultContext, repositoryModel, "is_private"))
	apiRequest(reader, readerGeneration, http.MethodGet, "/-/extensions/pages/pages/global/api/native/repository?id="+ownerAuthority.Repository.ID, http.StatusForbidden)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/pages/pages/global"), http.StatusOK)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/user/settings/extensions/pages/user"), http.StatusOK)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/admin/extensions/pages/site-admin"), http.StatusForbidden)
	ownerWorkspace := owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace"), http.StatusOK)
	require.Contains(t, ownerWorkspace.Body.String(), `data-extension-panel-id="status"`)
	readerWorkspace := reader.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace"), http.StatusOK)
	require.NotContains(t, readerWorkspace.Body.String(), `data-extension-panel-id="status"`)
	apiRequest(owner, ownerGeneration, http.MethodGet, "/-/extensions/panels/pages/status/api/context", http.StatusOK)
	apiRequest(reader, readerGeneration, http.MethodGet, "/-/extensions/panels/pages/status/api/context", http.StatusNotFound)
	siteAdmin := loginUser(t, "user1")
	siteAdmin.MakeRequest(t, NewRequest(t, http.MethodGet, "/admin/extensions/pages/site-admin"), http.StatusOK)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/assets/pages/main.js"), http.StatusOK)
	owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/extensions/pages/admin"), http.StatusOK)
	owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, "/org/create", map[string]string{
		"org_name":                      "native_callback_owner",
		"visibility":                    "0",
		"repo_admin_change_team_access": "on",
	}), http.StatusSeeOther)
	ownerPage := apiRequest(owner, ownerGeneration, http.MethodGet, "/-/extensions/pages/pages/global/api/native/repositories?query=repo1", http.StatusOK)
	var ownerRepositories extension.RepositoryPage
	require.NoError(t, json.Unmarshal(ownerPage.Body.Bytes(), &ownerRepositories))
	foundOwnedRepository := false
	for _, repository := range ownerRepositories.Items {
		require.Equal(t, "user2", repository.Owner)
		if repository.Name == "repo1" {
			foundOwnedRepository = true
		}
	}
	require.True(t, foundOwnedRepository)
	firstPageResponse := apiRequest(owner, ownerGeneration, http.MethodGet, "/-/extensions/pages/pages/global/api/native/repositories?query=pagination&limit=2", http.StatusOK)
	var firstPage extension.RepositoryPage
	require.NoError(t, json.Unmarshal(firstPageResponse.Body.Bytes(), &firstPage))
	require.Len(t, firstPage.Items, 2)
	require.Equal(t, "pagination-01", firstPage.Items[0].Name)
	require.Equal(t, "pagination-02", firstPage.Items[1].Name)
	require.NotEmpty(t, firstPage.NextCursor)
	secondPageResponse := apiRequest(owner, ownerGeneration, http.MethodGet, "/-/extensions/pages/pages/global/api/native/repositories?query=pagination&limit=2&cursor="+firstPage.NextCursor, http.StatusOK)
	var secondPage extension.RepositoryPage
	require.NoError(t, json.Unmarshal(secondPageResponse.Body.Bytes(), &secondPage))
	require.Len(t, secondPage.Items, 1)
	require.Equal(t, "pagination-03", secondPage.Items[0].Name)
	require.Empty(t, secondPage.NextCursor)
	readerPage := apiRequest(reader, readerGeneration, http.MethodGet, "/-/extensions/pages/pages/global/api/native/repositories?query=repo1", http.StatusOK)
	var readerRepositories extension.RepositoryPage
	require.NoError(t, json.Unmarshal(readerPage.Body.Bytes(), &readerRepositories))
	require.Empty(t, readerRepositories.Items)
	ownerCheck := apiRequest(owner, ownerGeneration, http.MethodGet, "/-/extensions/pages/pages/global/api/native/organization-owner?name=native_callback_owner", http.StatusOK)
	var ownsOrganization bool
	require.NoError(t, json.Unmarshal(ownerCheck.Body.Bytes(), &ownsOrganization))
	require.True(t, ownsOrganization)
	readerCheck := apiRequest(reader, readerGeneration, http.MethodGet, "/-/extensions/pages/pages/global/api/native/organization-owner?name=native_callback_owner", http.StatusOK)
	require.NoError(t, json.Unmarshal(readerCheck.Body.Bytes(), &ownsOrganization))
	require.False(t, ownsOrganization)
	missingOrganization := apiRequest(reader, readerGeneration, http.MethodGet, "/-/extensions/pages/pages/global/api/native/organization-owner?name=native_callback_absent", http.StatusOK)
	require.NoError(t, json.Unmarshal(missingOrganization.Body.Bytes(), &ownsOrganization))
	require.False(t, ownsOrganization)
	ownerKeysResponse := apiRequest(owner, ownerGeneration, http.MethodGet, "/-/extensions/pages/pages/global/api/native/public-keys", http.StatusOK)
	var ownerKeys []extension.PublicKey
	require.NoError(t, json.Unmarshal(ownerKeysResponse.Body.Bytes(), &ownerKeys))
	require.Len(t, ownerKeys, 1)
	readerKeysResponse := apiRequest(reader, readerGeneration, http.MethodGet, "/-/extensions/pages/pages/global/api/native/public-keys", http.StatusOK)
	var readerKeys []extension.PublicKey
	require.NoError(t, json.Unmarshal(readerKeysResponse.Body.Bytes(), &readerKeys))
	require.Empty(t, readerKeys)
}

func TestExtensionPreferredWorkspace(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	root, err := os.MkdirTemp("", "fe-")
	require.NoError(t, err)
	defer os.RemoveAll(root)
	packageDir := filepath.Join(root, "preferred")
	require.NoError(t, os.MkdirAll(filepath.Join(packageDir, "assets"), 0o700))
	executable, err := os.Executable()
	require.NoError(t, err)
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "'\n"
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "run"), []byte(script), 0o700))
	manifest := extension.Manifest{Protocol: extension.Protocol, ID: "preferred", Name: "Preferred", Version: "1", Executable: "run", PreferredWorkspace: true,
		Pages: []extension.Page{{ID: "home", Title: "Home", Scope: "global", Entry: "main.js"}}}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), encoded, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "assets", "main.js"), []byte("export function mount() {}"), 0o600))
	manager := runtime.NewManager(root)
	require.NoError(t, manager.Start(context.Background()))
	previous := runtime.GetManager()
	runtime.SetDefault(manager)
	defer func() {
		runtime.SetDefault(previous)
		if manager != nil {
			require.NoError(t, manager.Close())
		}
	}()
	owner := loginUser(t, "user2")
	marker := `data-extension-preferred-workspace="/-/extensions/workspace"`
	require.Contains(t, owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/explore/repos"), http.StatusOK).Body.String(), marker)
	require.NotContains(t, owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/?extension_workspace=off"), http.StatusOK).Body.String(), marker)
	require.NotContains(t, MakeRequest(t, NewRequest(t, http.MethodGet, "/explore/repos"), http.StatusOK).Body.String(), marker)
	require.NotContains(t, owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/user/settings/security"), http.StatusOK).Body.String(), marker)
	require.NotContains(t, owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/explore/repos?code=private"), http.StatusOK).Body.String(), marker)
	workspace := owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace"), http.StatusOK)
	require.NotContains(t, workspace.Body.String(), marker)
	generation := workspace.Header().Get(extension.SessionGenerationHeader)
	require.Len(t, generation, 43)
	require.Contains(t, workspace.Body.String(), `data-workspace-session-generation="`+generation+`"`)
	page := owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/pages/preferred/home"), http.StatusOK)
	require.Contains(t, page.Body.String(), `data-extension-session-generation="`+generation+`"`)
	check := owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace?session_check=1"), http.StatusNoContent)
	require.Equal(t, generation, check.Header().Get(extension.SessionGenerationHeader))
	require.Empty(t, check.Body.String())
	other := loginUser(t, "user4")
	otherCheck := other.MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace?session_check=1"), http.StatusNoContent)
	require.NotEqual(t, generation, otherCheck.Header().Get(extension.SessionGenerationHeader))
	MakeRequest(t, NewRequest(t, http.MethodGet, "/-/extensions/workspace?session_check=1"), http.StatusUnauthorized)
	require.NoError(t, manager.Close())
	manager = nil
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, ".disabled"), nil, 0o600))
	disabledManager := runtime.NewManager(root)
	require.NoError(t, disabledManager.Start(context.Background()))
	manager = disabledManager
	runtime.SetDefault(disabledManager)
	require.NotContains(t, owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/explore/repos"), http.StatusOK).Body.String(), marker)
}
