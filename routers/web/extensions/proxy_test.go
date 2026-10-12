// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/modules/setting"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func scopedPath(t *testing.T, star string) (string, bool) {
	t.Helper()
	return scopedExtensionPath(star)
}

func TestScopedExtensionPath(t *testing.T) {
	for star, want := range map[string]string{
		"":                  "/",
		"/":                 "/",
		"action":            "/action",
		"projects/42/items": "/projects/42/items",
		"file.txt":          "/file.txt",
		"a/":                "/a/",
	} {
		path, ok := scopedPath(t, star)
		require.True(t, ok, star)
		require.Equal(t, want, path, star)
	}
	for _, star := range []string{
		"../secret", "a/../../secret", "./a", "a/./b", "/etc/passwd",
		"a//b", `a\b`, "%2e%2e/secret", "%2E/secret", "a%2fb", "a%5cb", "a\nb", "a\tb",
	} {
		_, ok := scopedPath(t, star)
		require.False(t, ok, star)
	}
}

func withStar(req *http.Request, star string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", url.PathEscape(star))
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestProxyForwardsOnlyScopedUpstreamPathAndQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://forgejo/-/native/pages/demo/page/api/projects/42/items?state=open", nil)
	ctx, recorder := testContext(withStar(req, "projects/42/items"))
	proxyWithAdmission(ctx, roundTripFunc(func(out *http.Request) (*http.Response, error) {
		require.Equal(t, "http://extension/projects/42/items?state=open", out.URL.String())
		require.Equal(t, "extension", out.URL.Host)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	}), extension.Authority{ExtensionID: "demo"}, "", req.Context())
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, `{"ok":true}`, recorder.Body.String())
}

func TestProxyRejectsUnscopedUpstreamPathBeforeBackend(t *testing.T) {
	for _, star := range []string{"../host-secret", "a/../../host-secret", "%2e%2e/host-secret"} {
		called := false
		req := httptest.NewRequest(http.MethodGet, "http://forgejo/api", nil)
		ctx, recorder := testContext(withStar(req, star))
		proxyWithAdmission(ctx, roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
		}), extension.Authority{ExtensionID: "demo"}, "", req.Context())
		require.False(t, called, star)
		require.Equal(t, http.StatusBadRequest, recorder.Code, star)
	}
}

func TestProxyBackendFailureAndOversizedResponseAreBounded(t *testing.T) {
	ctx, recorder := testContext(httptest.NewRequest(http.MethodGet, "http://forgejo/api", nil))
	proxyWithAdmission(ctx, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("backend offline")
	}), extension.Authority{ExtensionID: "demo"}, "", ctx.Req.Context())
	require.Equal(t, http.StatusBadGateway, recorder.Code)

	ctx, recorder = testContext(httptest.NewRequest(http.MethodGet, "http://forgejo/api", nil))
	proxyWithAdmission(ctx, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxResponseBytes+1)))}, nil
	}), extension.Authority{ExtensionID: "demo"}, "", ctx.Req.Context())
	require.Equal(t, http.StatusBadGateway, recorder.Code)
}

type blockingBody struct {
	closed chan struct{}
	once   sync.Once
}

func (b *blockingBody) Read([]byte) (int, error) {
	<-b.closed
	return 0, errors.New("response body retired")
}

func (b *blockingBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestProxyRequestCancellationRetiresStalledBackendBody(t *testing.T) {
	body := &blockingBody{closed: make(chan struct{})}
	reached := make(chan struct{})
	var once sync.Once
	ctx, recorder := testContext(httptest.NewRequest(http.MethodGet, "http://forgejo/api", nil))
	requestContext, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		proxyWithAdmission(ctx, roundTripFunc(func(*http.Request) (*http.Response, error) {
			once.Do(func() { close(reached) })
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
		}), extension.Authority{ExtensionID: "demo"}, "", requestContext)
	}()
	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatal("proxy never reached the backend")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("request cancellation did not retire the stalled backend body")
	}
	require.Equal(t, http.StatusBadGateway, recorder.Code)
}

func TestValidAPIOriginScoping(t *testing.T) {
	previous := setting.AppURL
	setting.AppURL = "https://forge.example/base/"
	t.Cleanup(func() { setting.AppURL = previous })
	base := func(method string) *http.Request {
		r := httptest.NewRequest(method, "https://forge.example/base/api/action", nil)
		r.Header.Set("Origin", "https://forge.example")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		return r
	}
	require.True(t, validAPIOrigin(base(http.MethodPost)))
	require.True(t, validAPIOrigin(base(http.MethodGet)))
	safeWithoutOrigin := base(http.MethodGet)
	safeWithoutOrigin.Header.Del("Origin")
	require.True(t, validAPIOrigin(safeWithoutOrigin), "safe requests may omit Origin")
	for name, mutate := range map[string]func(*http.Request){
		"unsafe without origin": func(r *http.Request) { r.Header.Del("Origin") },
		"foreign origin":        func(r *http.Request) { r.Header.Set("Origin", "https://foreign.example") },
		"duplicate origin":      func(r *http.Request) { r.Header.Add("Origin", "https://forge.example") },
		"cross-site fetch":      func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		"duplicate fetch site":  func(r *http.Request) { r.Header.Add("Sec-Fetch-Site", "same-origin") },
	} {
		t.Run(name, func(t *testing.T) {
			r := base(http.MethodPost)
			mutate(r)
			require.False(t, validAPIOrigin(r))
		})
	}
}
