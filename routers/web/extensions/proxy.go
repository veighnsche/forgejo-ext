// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/models/unit"
	"forgejo.org/modules/setting"
	webcontext "forgejo.org/services/context"
	runtime "forgejo.org/services/extensions"
)

const maxResponseBytes = 8 << 20

func api(scope string, panel bool) func(*webcontext.Context) {
	return func(ctx *webcontext.Context) {
		if !validAPIOrigin(ctx.Req) {
			ctx.Error(http.StatusForbidden, "Same-origin extension request required")
			return
		}
		manager := runtime.GetManager()
		if manager == nil {
			ctx.NotFound("Extension", nil)
			return
		}
		var d runtime.Descriptor
		var transport http.RoundTripper
		var page extension.Page
		if panel {
			var ok bool
			d, transport, ok = manager.Lookup(ctx.Params("extension"))
			if !ok {
				ctx.NotFound("Extension", nil)
				return
			}
			found := false
			for _, p := range d.Manifest.Panels {
				if p.ID == ctx.Params("page") {
					found = true
					break
				}
			}
			if !found {
				ctx.NotFound("Extension panel", nil)
				return
			}
		} else {
			var found bool
			d, transport, page, found = findPage(ctx, scope)
			if !found {
				return
			}
		}
		kind := "page"
		if panel {
			kind = "panel"
		}
		authority := extension.Authority{
			ExtensionID:  d.Manifest.ID,
			Contribution: extension.Contribution{ID: ctx.Params("page"), Kind: kind, Scope: scope, Action: strings.ToLower(ctx.Req.Method)},
			Actor:        extension.Actor{ID: strconv.FormatInt(ctx.Doer.ID, 10), Username: ctx.Doer.Name, SiteAdmin: ctx.Doer.IsAdmin},
		}
		if scope == "repository" {
			authority.Repository = &extension.Repository{ID: strconv.FormatInt(ctx.Repo.Repository.ID, 10), Owner: ctx.Repo.Owner.Name, Name: ctx.Repo.Repository.Name, Permission: repositoryPermission(ctx)}
		}
		generation, err := sessionGeneration(ctx)
		if err != nil {
			ctx.Error(http.StatusServiceUnavailable, "Extension authority unavailable")
			return
		}
		providedGeneration := ctx.Req.Header.Get(extension.SessionGenerationHeader)
		if isWebSocket(ctx.Req) {
			// The private authority carries the native generation. The terminal
			// checks its first bounded JSON handshake against this value.
			providedGeneration = generation
		}
		if (!isWebSocket(ctx.Req) && len(ctx.Req.Header.Values(extension.SessionGenerationHeader)) != 1) || len(providedGeneration) != 43 || !hmac.Equal([]byte(providedGeneration), []byte(generation)) {
			ctx.Error(http.StatusConflict, "Extension page session changed")
			return
		}
		repositoryID := ""
		contribution := pageContribution(page, ctx.Req.Method)
		if panel {
			contribution = panelContribution(extension.Panel{ID: ctx.Params("page")}, ctx.Req.Method)
		} else if scope == "repository" {
			repositoryID = strconv.FormatInt(ctx.Repo.Repository.ID, 10)
		}
		if !authorizesContribution(ctx.Req.Context(), d, transport, contribution, repositoryID, ctx.Doer.ID) {
			ctx.NotFound("Extension contribution", nil)
			return
		}
		requestContext, cancel := extensionRequestContext(ctx.Req)
		defer cancel()
		token, admittedAuthority, err := createAdmission(requestContext, ctx, d, generation, authority)
		if err != nil {
			ctx.Error(http.StatusServiceUnavailable, "Extension authority unavailable")
			return
		}
		defer nativeAdmissions.revoke(token)
		if isWebSocket(ctx.Req) {
			proxyWebSocket(ctx, transport, admittedAuthority, token)
			return
		}
		proxyWithAdmission(ctx, transport, admittedAuthority, token, requestContext)
	}
}

// Ordinary responses are bounded and buffered so an extension cannot create an
// unbounded authenticated stream. Streaming requires independent session revocation.
func proxy(ctx *webcontext.Context, transport http.RoundTripper, authority extension.Authority) {
	requestContext, cancel := context.WithTimeout(ctx.Req.Context(), 30*time.Second)
	defer cancel()
	proxyWithAdmission(ctx, transport, authority, "", requestContext)
}

func proxyWithAdmission(ctx *webcontext.Context, transport http.RoundTripper, authority extension.Authority, admission string, requestContext context.Context) {
	if ctx.Req.Header.Get("Upgrade") != "" || strings.Contains(strings.ToLower(ctx.Req.Header.Get("Accept")), "text/event-stream") {
		ctx.Error(http.StatusNotImplemented, "Extension streaming is not enabled")
		return
	}
	var body io.ReadCloser = http.NoBody
	if ctx.Req.Body != nil && ctx.Req.Body != http.NoBody {
		body = http.MaxBytesReader(ctx.Resp, ctx.Req.Body, maxResponseBytes)
	}
	target := &url.URL{Scheme: "http", Host: "extension", Path: "/" + ctx.Params("*"), RawQuery: ctx.Req.URL.RawQuery}
	req, err := http.NewRequestWithContext(requestContext, ctx.Req.Method, target.String(), body)
	if err != nil {
		ctx.Error(http.StatusBadRequest)
		return
	}
	for _, name := range []string{"Accept", "Accept-Language", "Content-Type", "Origin", "Sec-Fetch-Site"} {
		if value := ctx.Req.Header.Get(name); value != "" {
			req.Header.Set(name, value)
		}
	}
	if authority.SessionGeneration != "" {
		req.Header.Set(extension.SessionGenerationHeader, authority.SessionGeneration)
	}
	encoded, _ := json.Marshal(authority)
	req.Header.Set(extension.ContextHeader, string(encoded))
	if admission != "" {
		req.Header.Set(extension.AdmissionHeader, admission)
	}
	response, err := transport.RoundTrip(req)
	if err != nil {
		ctx.Error(http.StatusBadGateway, "Extension unavailable")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 && response.StatusCode < 400 || strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		ctx.Error(http.StatusBadGateway, "Unsupported extension response")
		return
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		ctx.Error(http.StatusBadGateway, "Extension response exceeded limits")
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.Resp.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		ctx.Resp.Header().Set("Content-Type", contentType)
	}
	ctx.Resp.WriteHeader(response.StatusCode)
	_, _ = ctx.Resp.Write(data)
}

func repositoryPermission(ctx *webcontext.Context) string {
	if ctx.Repo.IsAdmin() {
		return "admin"
	}
	if ctx.Repo.CanWrite(unit.TypeCode) {
		return "write"
	}
	return "read"
}

// Mutations require one exact browser origin even when Fetch Metadata is absent.
// Safe requests may omit Origin, but supplied browser metadata must still agree.
func validAPIOrigin(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	unsafe := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
	if len(origins) > 1 || unsafe && len(origins) != 1 {
		return false
	}
	if len(origins) == 1 {
		expected, err := url.Parse(setting.AppURL)
		if err != nil || expected.Scheme == "" || expected.Host == "" || origins[0] != expected.Scheme+"://"+expected.Host {
			return false
		}
	}
	sites := r.Header.Values("Sec-Fetch-Site")
	return len(sites) == 0 || len(sites) == 1 && sites[0] == "same-origin"
}
