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
	"forgejo.org/modules/base"
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
		m.Methods("GET,HEAD,POST,PUT,PATCH,DELETE", prefix+"/{extension}/{page}/api/*", append(guards, api(scope, false))...)
	}
	register("/-/native/pages", "global", NativeSession, signedIn)
	register("/user/settings/-/native/pages", "user", NativeSession, signedIn)
	register("/admin/-/native/pages", "admin", NativeSession, admin)
	register("/{username}/{reponame}/-/native/pages", "repository", NativeSession, signedIn, context.RepoAssignment, context.UnitTypes(), Navigation)
	for _, scope := range []string{"global", "user", "admin"} {
		for _, route := range extension.NativePageRoutes(scope) {
			prefix := ""
			guard := signedIn
			if scope == "user" {
				prefix = "/user/settings"
			}
			if scope == "admin" {
				prefix = "/admin"
				guard = admin
			}
			m.Get(prefix+"/"+route, NativeSession, guard, pageRoute(scope, route))
		}
	}
	m.Get("/{username}/{reponame}/workspace", NativeSession, signedIn, context.RepoAssignment, context.UnitTypes(), Navigation, repositoryWorkspace)
	m.Get("/-/native/session", NativeSession, signedIn, SessionCheck)
	m.Methods("GET,HEAD", "/-/native/assets/{extension}/*", NativeSession, signedIn, Assets)
	m.Methods("GET,HEAD,POST,PUT,PATCH,DELETE", "/-/native/panels/{extension}/{page}/api/*", NativeSession, signedIn, api("panel", true))
	if setting.IsInTesting {
		m.Get("/-/native/test/session-stream", NativeSession, signedIn, sessionStreamTest)
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
	// Composition changes the successful HTML presentation, never the native
	// handler, URL, session, repository assignment or account-state guards.
	eligibleWorkspace := workspaceDocument(ctx)

	for _, listed := range manager.List() {
		descriptor, transport, ok := manager.Lookup(listed.Manifest.ID)
		if !ok {
			continue
		}
		if eligibleWorkspace && descriptor.Manifest.PreferredWorkspace {
			ctx.HTMLPresentation = func(status int, name base.TplName) base.TplName {
				if status != http.StatusOK || strings.HasPrefix(string(name), "user/auth/") || strings.HasPrefix(string(name), "status/") || strings.HasPrefix(string(name), "install/") {
					return name
				}
				return renderWorkspace(ctx, name)
			}
		}
		for _, p := range descriptor.Manifest.Pages {
			if p.Route == "" || !allowed(ctx, p) {
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
			if base == "" && p.Scope == "repository" {
				continue
			}
			pages[p.Scope] = append(pages[p.Scope], Link{ID: p.ID, Title: p.Title, ExtensionID: descriptor.Manifest.ID, Permission: p.Permission, URL: base + "/" + p.Route})
			if p.Scope == "global" && p.Route == "workspace" && ctx.Repo != nil && ctx.Repo.Repository != nil && ctx.Repo.CanRead(unit.TypeCode) {
				if _, _, _, exists := findRoute(ctx, "repository", "workspace"); !exists {
					pages["repository"] = append(pages["repository"], Link{ID: p.ID, Title: p.Title, ExtensionID: descriptor.Manifest.ID, Permission: "read", URL: ctx.Repo.RepoLink + "/workspace"})
				}
			}
		}
	}
}

func pageBase(ctx *context.Context, scope string) string {
	switch scope {
	case "global":
		return setting.AppSubURL
	case "user":
		return setting.AppSubURL + "/user/settings"
	case "admin":
		return setting.AppSubURL + "/admin"
	case "repository":
		if ctx.Repo != nil && ctx.Repo.Repository != nil {
			return ctx.Repo.RepoLink
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
	ctx.NotFound("Page", nil)
	return runtime.Descriptor{}, nil, extension.Page{}, false
}

// Public pages resolve host-owned routes without exposing package identities.
func pageRoute(scope, route string) func(*context.Context) {
	return func(ctx *context.Context) {
		d, transport, p, ok := findRoute(ctx, scope, route)
		if !ok {
			ctx.NotFound("Page", nil)
			return
		}
		renderPage(ctx, d, transport, p)
	}
}

func findRoute(ctx *context.Context, scope, route string) (runtime.Descriptor, http.RoundTripper, extension.Page, bool) {
	if manager := runtime.GetManager(); manager != nil {
		for _, listed := range manager.List() {
			d, transport, ok := manager.Lookup(listed.Manifest.ID)
			if !ok {
				continue
			}
			for _, p := range d.Manifest.Pages {
				if p.Scope == scope && p.Route == route && allowed(ctx, p) {
					return d, transport, p, true
				}
			}
		}
	}
	return runtime.Descriptor{}, nil, extension.Page{}, false
}

func repositoryWorkspace(ctx *context.Context) {
	if !ctx.Repo.CanRead(unit.TypeCode) {
		ctx.NotFound("Workspace", nil)
		return
	}
	d, transport, p, ok := findRoute(ctx, "repository", "workspace")
	if !ok {
		d, transport, p, ok = findRoute(ctx, "global", "workspace")
	}
	if !ok {
		ctx.NotFound("Workspace", nil)
		return
	}
	ctx.Data["ExtensionRepositoryID"] = strconv.FormatInt(ctx.Repo.Repository.ID, 10)
	// Observed repository metadata chooses the display only. A global contribution
	// retains its global authority; each product API still checks its target.
	renderPage(ctx, d, transport, p)
}

func pageAPIBase(ctx *context.Context, d runtime.Descriptor, p extension.Page) string {
	prefix := setting.AppSubURL
	switch p.Scope {
	case "user":
		prefix += "/user/settings"
	case "admin":
		prefix += "/admin"
	case "repository":
		prefix = ctx.Repo.RepoLink
	}
	return prefix + "/-/native/pages/" + d.Manifest.ID + "/" + p.ID + "/api/"
}

func renderPage(ctx *context.Context, d runtime.Descriptor, transport http.RoundTripper, p extension.Page) {
	generation, err := sessionGeneration(ctx)
	if err != nil {
		ctx.Error(http.StatusServiceUnavailable, "Session authority unavailable")
		return
	}
	repositoryID := ""
	if p.Scope == "repository" {
		repositoryID = strconv.FormatInt(ctx.Repo.Repository.ID, 10)
	}
	if !authorizesContribution(ctx.Req.Context(), d, transport, pageContribution(p, http.MethodGet), repositoryID, ctx.Doer.ID) {
		ctx.NotFound("Page", nil)
		return
	}
	assetBase := setting.AppSubURL + "/-/native/assets/" + d.Manifest.ID + "/"
	ctx.Data["Title"] = p.Title
	ctx.Data["PageIsUserSettings"] = p.Scope == "user"
	ctx.Data["PageIsAdmin"] = p.Scope == "admin"
	ctx.Data["ExtensionScope"] = p.Scope
	ctx.Data["ExtensionRepoSettings"] = p.Scope == "repository" && p.Permission == "admin"
	ctx.Data["PageIsRepoSettings"] = p.Scope == "repository" && p.Permission == "admin"
	ctx.Data["Extension"] = map[string]string{"ID": d.Manifest.ID, "Name": d.Manifest.Name, "PageID": p.ID, "Title": p.Title}
	ctx.Data["ExtensionAPIBase"] = pageAPIBase(ctx, d, p)
	ctx.Data["ExtensionAssetBase"] = assetBase
	ctx.Data["ExtensionEntry"] = assetBase + p.Entry
	ctx.Data["ExtensionSessionGeneration"] = generation
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.Resp.Header().Set(extension.SessionGenerationHeader, generation)
	ctx.Data["PageIsExtension"] = true
	ctx.Data["ExtensionPageURL"] = setting.AppSubURL + ctx.Req.URL.EscapedPath()
	ctx.PageData["extension"] = map[string]string{"id": d.Manifest.ID, "name": d.Manifest.Name, "pageID": p.ID, "title": p.Title}
	ctx.PageData["apiBase"] = pageAPIBase(ctx, d, p)
	ctx.PageData["assetBase"] = assetBase
	ctx.PageData["entry"] = assetBase + p.Entry
	ctx.HTML(http.StatusOK, "extensions/page")
}

func SessionCheck(ctx *context.Context) {
	generation, err := sessionGeneration(ctx)
	if err != nil {
		ctx.Error(http.StatusServiceUnavailable, "Session authority unavailable")
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.Resp.Header().Set(extension.SessionGenerationHeader, generation)
	ctx.Resp.WriteHeader(http.StatusNoContent)
}

func workspaceDocument(ctx *context.Context) bool {
	if ctx.Req.Method != http.MethodGet || !validWorkspacePath(setting.AppSubURL+ctx.Req.URL.RequestURI()) {
		return false
	}
	if ctx.Req.Header.Get("HX-Request") == "true" || ctx.Req.Header.Get("X-Requested-With") != "" {
		return false
	}
	dest := ctx.Req.Header.Get("Sec-Fetch-Dest")
	return dest == "document" || dest == "" && strings.Contains(ctx.Req.Header.Get("Accept"), "text/html")
}

func renderWorkspace(ctx *context.Context, original base.TplName) base.TplName {
	manager := runtime.GetManager()
	if manager == nil {
		return original
	}
	generation, err := sessionGeneration(ctx)
	if err != nil {
		return original
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
			assetBase := setting.AppSubURL + "/-/native/assets/" + d.Manifest.ID + "/"
			apiBase := setting.AppSubURL + "/-/native/panels/" + d.Manifest.ID + "/" + p.ID + "/api/"
			panels = append(panels, Link{ID: p.ID, Title: p.Title, ExtensionID: d.Manifest.ID, Entry: assetBase + p.Entry, APIBase: apiBase, AssetBase: assetBase})
		}
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.Resp.Header().Set(extension.SessionGenerationHeader, generation)
	ctx.Data["WorkspacePath"] = setting.AppSubURL + ctx.Req.URL.RequestURI()
	ctx.Data["WorkspaceSessionGeneration"] = generation
	ctx.Data["PageIsExtensionWorkspace"] = true
	ctx.Data["ExtensionPanels"] = panels
	ctx.PageData["extensionPanels"] = panels
	return "extensions/workspace"
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
	if workspaceAuthPath.MatchString(clean) || strings.Contains(clean, "/-/native/") {
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
		ctx.NotFound("Page", nil)
		return
	}
	d, _, ok := manager.Lookup(ctx.Params("extension"))
	if !ok {
		ctx.NotFound("Page", nil)
		return
	}
	root, err := os.OpenRoot(filepath.Join(d.Root, "assets"))
	if err != nil {
		ctx.NotFound("Asset", nil)
		return
	}
	defer root.Close()
	name := ctx.Params("*")
	file, err := root.Open(name)
	if err != nil {
		ctx.NotFound("Asset", nil)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		ctx.NotFound("Asset", nil)
		return
	}
	ctx.Resp.Header().Set("X-Content-Type-Options", "nosniff")
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	http.ServeContent(ctx.Resp, ctx.Req, name, stat.ModTime(), file)
}
