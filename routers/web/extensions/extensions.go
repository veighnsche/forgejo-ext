// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/websocket"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/models/unit"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/web"
	"forgejo.org/services/context"
	runtime "forgejo.org/services/extensions"
)

// NativeSession excludes API and reverse-proxy credentials from the extension surface.
// The session uid must be the actor selected by Forgejo's authentication chain.
func NativeSession(ctx *context.Context) {
	if !nativeSession(ctx) {
		ctx.Error(http.StatusUnauthorized)
	}
}

func nativeSession(ctx *context.Context) bool {
	if ctx.Session == nil || ctx.Doer == nil || ctx.Req.Header.Get("Authorization") != "" || ctx.Req.URL.Query().Has("token") || ctx.Req.URL.Query().Has("access_token") {
		return false
	}
	uid, ok := ctx.Session.Get("uid").(int64)
	return ok && uid == ctx.Doer.ID && ctx.Authentication != nil && !ctx.Authentication.IsReverseProxyAuthentication()
}

// Register keeps the existing native account-state and cross-origin middleware.
func Register(m *web.Route, signedIn, admin any) {
	register := func(prefix, scope string, guards ...any) {
		m.Group(prefix, func() {
			m.Get("/{extension}/{page}", page(scope))
			m.Methods("GET,HEAD,POST,PUT,PATCH,DELETE", "/{extension}/{page}/api/*", api(scope, false))
		}, guards...)
	}
	register("/-/extensions/pages", "global", NativeSession, signedIn)
	register("/user/settings/extensions", "user", NativeSession, signedIn)
	register("/admin/extensions", "admin", NativeSession, admin)
	register("/{username}/{reponame}/extensions", "repository", NativeSession, signedIn, context.RepoAssignment, context.UnitTypes(), Navigation)
	m.Get("/-/extensions/workspace", NativeSession, signedIn, Workspace)
	m.Methods("GET,HEAD", "/-/extensions/assets/{extension}/*", NativeSession, signedIn, Assets)
	m.Methods("GET,HEAD,POST,PUT,PATCH,DELETE", "/-/extensions/panels/{extension}/{page}/api/*", NativeSession, signedIn, api("panel", true))
	if setting.IsInTesting {
		// Exercise the real session, context, authentication, compression, and
		// response-writer chain without exposing a diagnostic endpoint in Forgejo.
		m.Get("/-/extensions/test/session-stream", NativeSession, signedIn, sessionStreamTest)
	}
}

func sessionStreamTest(ctx *context.Context) {
	websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()
		for {
			var value string
			if err := websocket.Message.Receive(conn, &value); err != nil {
				return
			}
			if err := websocket.Message.Send(conn, value); err != nil {
				return
			}
		}
	}).ServeHTTP(ctx.Resp, ctx.Req)
}

type Link struct{ ID, Title, ExtensionID, URL, Entry, APIBase, AssetBase, Permission string }

// Navigation also runs after repository assignment, when its permission is known.
func Navigation(ctx *context.Context) {
	pages := map[string][]Link{}
	ctx.Data["ExtensionPages"] = pages
	manager := runtime.GetManager()
	if manager == nil || !nativeSession(ctx) {
		return
	}
	// Only a running, enabled package may prefer the generic workspace. The
	// browser receives the host route, never a package-selected destination.
	eligibleWorkspace := ctx.Req.Method == http.MethodGet && ctx.Req.URL.Query().Get("extension_workspace") != "off" && validWorkspacePath(ctx.Req.URL.RequestURI())
	for _, listed := range manager.List() {
		descriptor, transport, ok := manager.Lookup(listed.Manifest.ID)
		if !ok {
			continue
		}
		if eligibleWorkspace && descriptor.Manifest.PreferredWorkspace {
			ctx.Data["ExtensionPreferredWorkspace"] = setting.AppSubURL + "/-/extensions/workspace"
		}
		for _, p := range descriptor.Manifest.Pages {
			if !allowed(ctx, p) {
				continue
			}
			repositoryID := ""
			if p.Scope == "repository" {
				repositoryID = strconv.FormatInt(ctx.Repo.Repository.ID, 10)
			}
			if !authorizesContribution(ctx.Req.Context(), descriptor, transport, pageContribution(p, http.MethodGet), repositoryID, ctx.Doer.ID) {
				continue
			}
			base := pageBase(ctx, p.Scope)
			if base == "" {
				continue
			}
			pages[p.Scope] = append(pages[p.Scope], Link{ID: p.ID, Title: p.Title, ExtensionID: descriptor.Manifest.ID, Permission: p.Permission, URL: base + "/" + descriptor.Manifest.ID + "/" + p.ID})
		}
	}
	ctx.Data["HasExtensionPanels"] = hasPanels(manager)
}

func hasPanels(manager *runtime.Manager) bool {
	for _, d := range manager.List() {
		if len(d.Manifest.Panels) != 0 {
			return true
		}
	}
	return false
}

func pageBase(ctx *context.Context, scope string) string {
	switch scope {
	case "global":
		return setting.AppSubURL + "/-/extensions/pages"
	case "user":
		return setting.AppSubURL + "/user/settings/extensions"
	case "admin":
		return setting.AppSubURL + "/admin/extensions"
	case "repository":
		if ctx.Repo != nil && ctx.Repo.Repository != nil {
			return ctx.Repo.RepoLink + "/extensions"
		}
	}
	return ""
}

func allowed(ctx *context.Context, p extension.Page) bool {
	switch p.Scope {
	case "global", "user":
		return true
	case "admin":
		return ctx.Doer.IsAdmin
	case "repository":
		if ctx.Repo == nil || ctx.Repo.Repository == nil {
			return false
		}
		switch p.Permission {
		case "read":
			return ctx.Repo.CanRead(unit.TypeCode)
		case "write":
			return ctx.Repo.CanWrite(unit.TypeCode)
		case "admin":
			return ctx.Repo.IsAdmin()
		}
	}
	return false
}

func findPage(ctx *context.Context, scope string) (runtime.Descriptor, http.RoundTripper, extension.Page, bool) {
	manager := runtime.GetManager()
	if manager != nil {
		if d, transport, ok := manager.Lookup(ctx.Params("extension")); ok {
			for _, p := range d.Manifest.Pages {
				if p.ID == ctx.Params("page") && p.Scope == scope && allowed(ctx, p) {
					return d, transport, p, true
				}
			}
		}
	}
	ctx.NotFound("Extension page", nil)
	return runtime.Descriptor{}, nil, extension.Page{}, false
}

func page(scope string) func(*context.Context) {
	return func(ctx *context.Context) {
		d, transport, p, ok := findPage(ctx, scope)
		if !ok {
			return
		}
		generation, err := sessionGeneration(ctx)
		if err != nil {
			ctx.Error(http.StatusServiceUnavailable, "Extension authority unavailable")
			return
		}
		repositoryID := ""
		if scope == "repository" {
			repositoryID = strconv.FormatInt(ctx.Repo.Repository.ID, 10)
		}
		if !authorizesContribution(ctx.Req.Context(), d, transport, pageContribution(p, http.MethodGet), repositoryID, ctx.Doer.ID) {
			ctx.NotFound("Extension page", nil)
			return
		}
		base := pageBase(ctx, scope) + "/" + d.Manifest.ID + "/" + p.ID
		assetBase := setting.AppSubURL + "/-/extensions/assets/" + d.Manifest.ID + "/"
		ctx.Data["Title"] = p.Title
		ctx.Data["PageIsUserSettings"] = scope == "user"
		ctx.Data["PageIsAdmin"] = scope == "admin"
		ctx.Data["ExtensionScope"] = scope
		ctx.Data["ExtensionRepoSettings"] = scope == "repository" && p.Permission == "admin"
		ctx.Data["PageIsRepoSettings"] = scope == "repository" && p.Permission == "admin"
		ctx.Data["Extension"] = map[string]string{"ID": d.Manifest.ID, "Name": d.Manifest.Name, "PageID": p.ID, "Title": p.Title}
		ctx.Data["ExtensionAPIBase"] = base + "/api/"
		ctx.Data["ExtensionAssetBase"] = assetBase
		ctx.Data["ExtensionEntry"] = assetBase + p.Entry
		ctx.Data["ExtensionSessionGeneration"] = generation
		ctx.Resp.Header().Set("Cache-Control", "no-store")
		ctx.Resp.Header().Set(extension.SessionGenerationHeader, generation)
		ctx.Data["PageIsExtension"] = true
		ctx.Data["ExtensionPageURL"] = base
		ctx.PageData["extension"] = map[string]string{"id": d.Manifest.ID, "name": d.Manifest.Name, "pageID": p.ID, "title": p.Title}
		ctx.PageData["apiBase"] = base + "/api/"
		ctx.PageData["assetBase"] = assetBase
		ctx.PageData["entry"] = assetBase + p.Entry
		ctx.HTML(http.StatusOK, "extensions/page")
	}
}

func Workspace(ctx *context.Context) {
	manager := runtime.GetManager()
	if manager == nil {
		ctx.NotFound("Extensions", nil)
		return
	}
	generation, err := sessionGeneration(ctx)
	if err != nil {
		ctx.Error(http.StatusServiceUnavailable, "Extension authority unavailable")
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.Resp.Header().Set(extension.SessionGenerationHeader, generation)
	if ctx.Req.URL.Query().Get("session_check") == "1" {
		ctx.Resp.WriteHeader(http.StatusNoContent)
		return
	}
	path := ctx.FormString("path")
	if !validWorkspacePath(path) {
		path = setting.AppSubURL + "/"
	}
	panels := []Link{}
	for _, listed := range manager.List() {
		d, transport, ok := manager.Lookup(listed.Manifest.ID)
		if !ok {
			continue
		}
		for _, p := range d.Manifest.Panels {
			if !authorizesContribution(ctx.Req.Context(), d, transport, panelContribution(p, http.MethodGet), "", ctx.Doer.ID) {
				continue
			}
			assetBase := setting.AppSubURL + "/-/extensions/assets/" + d.Manifest.ID + "/"
			apiBase := setting.AppSubURL + "/-/extensions/panels/" + d.Manifest.ID + "/" + p.ID + "/api/"
			panels = append(panels, Link{ID: p.ID, Title: p.Title, ExtensionID: d.Manifest.ID, Entry: assetBase + p.Entry, APIBase: apiBase, AssetBase: assetBase})
		}
	}
	ctx.Data["Title"] = "Workspace"
	ctx.Data["WorkspacePath"] = path
	ctx.Data["WorkspaceSessionGeneration"] = generation
	ctx.Data["PageIsExtensionWorkspace"] = true
	ctx.Data["ExtensionPanels"] = panels
	ctx.PageData["extensionPanels"] = panels
	ctx.HTML(http.StatusOK, "extensions/workspace")
}

var workspaceAuthPath = regexp.MustCompile(`(^|/)user/(login|logout|sign_up|forgot_password|forget_password|reset_password|recover_account|activate|activate_email|two_factor|webauthn|oauth2|openid|link_account|link_account_signin|link_account_signup)(/|$)|(^|/)user/settings/(change_password|security|applications|keys)(/|$)|(^|/)(install|login/oauth|login/openid|oauth2|openid)(/|$)`)
var workspaceCredentialKey = regexp.MustCompile(`(?i)^(token|password|secret|client_secret|authorization|auth|code|credential|session|api_key|private_key|.*_token)$`)

func validWorkspacePath(path string) bool {
	u, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(path, setting.AppSubURL+"/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\r\n") || u.Host != "" || u.Scheme != "" {
		return false
	}
	escaped := strings.ToLower(u.EscapedPath())
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") || strings.ContainsAny(u.Path, "\\\r\n") {
		return false
	}
	clean := pathpkg.Clean(u.Path)
	if setting.AppSubURL != "" && clean != setting.AppSubURL && !strings.HasPrefix(clean, setting.AppSubURL+"/") {
		return false
	}
	if workspaceAuthPath.MatchString(clean) || strings.HasSuffix(clean, "/-/extensions/workspace") || strings.Contains(clean, "/-/extensions/workspace/") {
		return false
	}
	for key := range u.Query() {
		if workspaceCredentialKey.MatchString(key) {
			return false
		}
	}
	return true
}

func Assets(ctx *context.Context) {
	manager := runtime.GetManager()
	if manager == nil {
		ctx.NotFound("Extension", nil)
		return
	}
	d, _, ok := manager.Lookup(ctx.Params("extension"))
	if !ok {
		ctx.NotFound("Extension", nil)
		return
	}
	root, err := os.OpenRoot(filepath.Join(d.Root, "assets"))
	if err != nil {
		ctx.NotFound("Extension asset", nil)
		return
	}
	defer root.Close()
	name := ctx.Params("*")
	file, err := root.Open(name)
	if err != nil {
		ctx.NotFound("Extension asset", nil)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		ctx.NotFound("Extension asset", nil)
		return
	}
	ctx.Resp.Header().Set("X-Content-Type-Options", "nosniff")
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	http.ServeContent(ctx.Resp, ctx.Req, name, stat.ModTime(), file)
}
