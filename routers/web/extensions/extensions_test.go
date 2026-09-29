// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forgejo.org/models/perm"
	access "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	extension "forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"
	"forgejo.org/services/auth"
	"forgejo.org/services/context"

	"code.forgejo.org/go-chi/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sessionUID struct {
	session.Store
	uid any
}

func (s sessionUID) Get(key any) any {
	if key == "uid" {
		return s.uid
	}
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testContext(req *http.Request) (*context.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	return &context.Context{Base: &context.Base{Req: req, Resp: context.WrapResponseWriter(recorder)}, Doer: &user_model.User{ID: 42}, Authentication: &auth.UnauthenticatedResult{}}, recorder
}

func TestNativeSessionAuthority(t *testing.T) {
	ctx, _ := testContext(httptest.NewRequest("GET", "http://forgejo/-/extensions/pages/demo/page", nil))
	require.False(t, nativeSession(ctx), "an authenticated actor without a native session is insufficient")
	ctx.Session = sessionUID{uid: int64(42)}
	require.True(t, nativeSession(ctx))
	ctx.Session = sessionUID{uid: int64(43)}
	require.False(t, nativeSession(ctx), "session identity must match selected actor")
	ctx.Session = sessionUID{uid: int64(42)}
	ctx.Req.Header.Set("Authorization", "Basic credential")
	require.False(t, nativeSession(ctx))
	ctx.Req.Header.Del("Authorization")
	ctx.Req.URL.RawQuery = "access_token=credential"
	require.False(t, nativeSession(ctx))
}

func TestRepositoryPagePermissions(t *testing.T) {
	ctx, _ := testContext(httptest.NewRequest("GET", "http://forgejo/", nil))
	ctx.Repo = &context.Repository{Repository: &repo_model.Repository{}, Permission: access.Permission{AccessMode: perm.AccessModeRead, UnitsMode: map[unit.Type]perm.AccessMode{unit.TypeCode: perm.AccessModeRead}}}
	p := extension.Page{Scope: "repository", Permission: "read"}
	require.True(t, allowed(ctx, p))
	p.Permission = "write"
	require.False(t, allowed(ctx, p))
	ctx.Repo.UnitsMode[unit.TypeCode] = perm.AccessModeWrite
	require.True(t, allowed(ctx, p))
	p.Permission = "admin"
	require.False(t, allowed(ctx, p))
	ctx.Repo.AccessMode = perm.AccessModeAdmin
	require.True(t, allowed(ctx, p))
	ctx.Repo.UnitsMode = map[unit.Type]perm.AccessMode{}
	p.Permission = "read"
	require.False(t, allowed(ctx, p))
	require.False(t, allowed(ctx, extension.Page{Scope: "admin"}))
}

func TestProxyReplacesAuthorityAndRemovesCredentials(t *testing.T) {
	req := httptest.NewRequest("POST", "http://forgejo/-/extensions/pages/demo/page/api/action", strings.NewReader("request"))
	req.Header.Set("Cookie", "session=private")
	req.Header.Set("Authorization", "Bearer private")
	req.Header.Set(extension.ContextHeader, `{"actor":{"id":1}}`)
	req.Header.Set("X-Forwarded-User", "admin")
	ctx, recorder := testContext(req)
	authority := extension.RequestAuthority{ExtensionID: "demo", PageID: "page", Scope: "global", Actor: extension.Actor{ID: 42, Username: "synthetic-user"}}
	proxy(ctx, roundTripFunc(func(out *http.Request) (*http.Response, error) {
		actual, err := extension.RequestContext(out)
		require.NoError(t, err)
		assert.Equal(t, authority, actual)
		assert.Empty(t, out.Header.Get("Cookie"))
		assert.Empty(t, out.Header.Get("Authorization"))
		assert.Empty(t, out.Header.Get("X-Forwarded-User"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "Set-Cookie": {"session=evil"}, "Location": {"https://evil.invalid"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	}), authority)
	assert.Equal(t, 200, recorder.Code)
	assert.Equal(t, `{"ok":true}`, recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("Set-Cookie"))
	assert.Empty(t, recorder.Header().Get("Location"))
}

func TestWorkspaceRejectsForeignAndRecursiveURLs(t *testing.T) {
	for _, path := range []string{"https://evil.invalid/", "//evil.invalid/", "/\\evil.invalid/", "/-/extensions/workspace?path=/", "/user/login", "/user/recover_account", "/user/oauth2/provider", "/install", "/repo?access_token=private", "/repo?PASSWORD=private", "/%2fevil.invalid/", "/%5cevil.invalid/"} {
		assert.False(t, validWorkspacePath(path), path)
	}
	assert.True(t, validWorkspacePath("/user2/repo1/issues"))
}

func TestProxyRejectsStreamingAndRedirects(t *testing.T) {
	authority := extension.RequestAuthority{ExtensionID: "demo", Scope: "global"}
	for _, header := range []string{"Upgrade", "Accept"} {
		req := httptest.NewRequest("GET", "http://forgejo/", nil)
		value := "websocket"
		if header == "Accept" {
			value = "text/event-stream"
		}
		req.Header.Set(header, value)
		ctx, recorder := testContext(req)
		proxy(ctx, roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("stream must not reach backend"); return nil, nil }), authority)
		assert.Equal(t, http.StatusNotImplemented, recorder.Code)
	}
	for _, test := range []struct {
		name        string
		status      int
		contentType string
	}{
		{"redirect", http.StatusFound, "text/plain"},
		{"unsolicited event stream", http.StatusOK, "text/event-stream"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := testContext(httptest.NewRequest("GET", "http://forgejo/", nil))
			proxy(ctx, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": {test.contentType}}, Body: io.NopCloser(strings.NewReader("body"))}, nil
			}), authority)
			assert.Equal(t, http.StatusBadGateway, recorder.Code)
		})
	}
}

func TestWorkspaceStaysInsideSubURL(t *testing.T) {
	previous := setting.AppSubURL
	setting.AppSubURL = "/forge"
	defer func() { setting.AppSubURL = previous }()
	assert.True(t, validWorkspacePath("/forge/team/repo"))
	assert.False(t, validWorkspacePath("/forge/../outside"))
	assert.False(t, validWorkspacePath("/forge/%2e%2e/outside"))
	assert.False(t, validWorkspacePath("/forge/user/login"))
	assert.False(t, validWorkspacePath("/forge/team?client_secret=private"))
}
