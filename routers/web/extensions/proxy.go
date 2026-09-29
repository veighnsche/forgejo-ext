// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"forgejo.org/models/unit"
	extension "forgejo.org/modules/extensions"
	webcontext "forgejo.org/services/context"
	runtime "forgejo.org/services/extensions"
)

const maxResponseBytes = 8 << 20

func api(scope string, panel bool) func(*webcontext.Context) {
	return func(ctx *webcontext.Context) {
		manager := runtime.GetManager()
		if manager == nil {
			ctx.NotFound("Extension", nil)
			return
		}
		d, transport, ok := manager.Lookup(ctx.Params("extension"))
		if !ok {
			ctx.NotFound("Extension", nil)
			return
		}
		if panel {
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
			_, _, found := findPage(ctx, scope)
			if !found {
				return
			}
		}
		authority := extension.RequestAuthority{ExtensionID: d.Manifest.ID, PageID: ctx.Params("page"), Scope: scope, Actor: extension.Actor{ID: ctx.Doer.ID, Username: ctx.Doer.Name, SiteAdmin: ctx.Doer.IsAdmin}}
		if scope == "repository" {
			authority.Repository = &extension.Repository{ID: ctx.Repo.Repository.ID, Owner: ctx.Repo.Owner.Name, Name: ctx.Repo.Repository.Name, Permission: repositoryPermission(ctx)}
		}
		proxy(ctx, transport, authority)
	}
}

// Ordinary responses are bounded and buffered so an extension cannot create an
// unbounded authenticated stream. Streaming requires independent session revocation.
func proxy(ctx *webcontext.Context, transport http.RoundTripper, authority extension.RequestAuthority) {
	if ctx.Req.Header.Get("Upgrade") != "" || strings.Contains(strings.ToLower(ctx.Req.Header.Get("Accept")), "text/event-stream") {
		ctx.Error(http.StatusNotImplemented, "Extension streaming is not enabled")
		return
	}
	requestContext, cancel := context.WithTimeout(ctx.Req.Context(), 30*time.Second)
	defer cancel()
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
	for _, name := range []string{"Accept", "Accept-Language", "Content-Type"} {
		if value := ctx.Req.Header.Get(name); value != "" {
			req.Header.Set(name, value)
		}
	}
	encoded, _ := json.Marshal(authority)
	req.Header.Set(extension.ContextHeader, string(encoded))
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
