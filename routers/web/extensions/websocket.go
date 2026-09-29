// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/modules/graceful"
	"forgejo.org/modules/setting"
	webcontext "forgejo.org/services/context"

	"github.com/coder/websocket"
)

const (
	streamHeartbeat     = 15 * time.Second
	streamCheckTimeout  = 5 * time.Second
	streamMessageBytes  = 32768
	maxExtensionStreams = 128
)

// This bounds host-owned streams independently of each application's own cap.
var extensionStreams = make(chan struct{}, maxExtensionStreams)

// Session providers expose a blocking Read API. Retain this check slot until
// Read actually returns, even when the associated transport has been closed.
var streamAuthorityChecks = make(chan struct{}, maxExtensionStreams)

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func extensionRequestContext(r *http.Request) (context.Context, context.CancelFunc) {
	if isWebSocket(r) {
		return context.WithCancel(r.Context())
	}
	return context.WithTimeout(r.Context(), 30*time.Second)
}

func validStreamOrigin(r *http.Request) bool {
	expected, err := url.Parse(setting.AppURL)
	if err != nil || expected.Scheme == "" || expected.Host == "" {
		return false
	}
	origins := r.Header.Values("Origin")
	if r.Method != http.MethodGet || len(origins) != 1 || origins[0] != expected.Scheme+"://"+expected.Host || r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Sec-WebSocket-Protocol")) != 0 {
		return false
	}
	for name, value := range map[string]string{"Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "websocket", "Sec-Fetch-Dest": "empty"} {
		values := r.Header.Values(name)
		if len(values) > 1 || len(values) == 1 && values[0] != value {
			return false
		}
	}
	return true
}

func proxyWebSocket(ctx *webcontext.Context, transport http.RoundTripper, authority extension.Authority, admission string) {
	if !validStreamOrigin(ctx.Req) {
		ctx.Error(http.StatusForbidden, "Same-origin extension stream required")
		return
	}
	entry := nativeAdmissions.get(admission)
	if entry == nil {
		ctx.Error(http.StatusUnauthorized)
		return
	}
	select {
	case extensionStreams <- struct{}{}:
		defer func() { <-extensionStreams }()
	default:
		ctx.Error(http.StatusServiceUnavailable, "Extension stream limit reached")
		return
	}
	streamCtx, cancel := context.WithCancel(entry.requestCtx)
	defer cancel()
	stopShutdown := context.AfterFunc(graceful.GetManager().ShutdownContext(), cancel)
	defer stopShutdown()
	check, checked := context.WithTimeout(streamCtx, streamCheckTimeout)
	valid := boundedStreamAuthority(check, entry)
	checked()
	if !valid {
		ctx.Error(http.StatusUnauthorized, "Extension stream authority unavailable")
		return
	}
	headers := make(http.Header)
	encoded, _ := json.Marshal(authority)
	headers.Set(extension.ContextHeader, string(encoded))
	headers.Set(extension.AdmissionHeader, admission)
	// Origin is a host-validated protocol input, not authority supplied by the app.
	headers.Set("Origin", ctx.Req.Header.Get("Origin"))
	target := (&url.URL{Scheme: "ws", Host: "extension", Path: "/" + ctx.Params("*")}).String()
	handshake, done := context.WithTimeout(streamCtx, streamCheckTimeout)
	backend, response, err := websocket.Dial(handshake, target, &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, HTTPHeader: headers, CompressionMode: websocket.CompressionDisabled})
	done()
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		ctx.Error(http.StatusBadGateway, "Extension stream unavailable")
		return
	}
	defer backend.CloseNow()
	browser, err := websocket.Accept(ctx.Resp, ctx.Req, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer browser.CloseNow()
	browser.SetReadLimit(streamMessageBytes)
	backend.SetReadLimit(streamMessageBytes)
	stop := context.AfterFunc(streamCtx, func() { _ = browser.CloseNow(); _ = backend.CloseNow() })
	defer stop()
	go monitorStream(streamCtx, cancel, entry, browser, backend)
	finished := make(chan struct{})
	go func() { defer close(finished); defer cancel(); relayStream(streamCtx, backend, browser) }()
	relayStream(streamCtx, browser, backend)
	cancel()
	<-finished
}

// One message per direction is resident at a time. A slow destination blocks its
// source and a five-second write deadline bounds that pressure without a queue.
func relayStream(ctx context.Context, source, target *websocket.Conn) {
	for {
		kind, body, err := source.Read(ctx)
		if err != nil {
			return
		}
		write, done := context.WithTimeout(ctx, streamCheckTimeout)
		err = target.Write(write, kind, body)
		done()
		if err != nil {
			return
		}
	}
}

func monitorStream(ctx context.Context, cancel context.CancelFunc, entry *callbackAdmission, browser, backend *websocket.Conn) {
	ticker := time.NewTicker(streamHeartbeat)
	defer ticker.Stop()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check, done := context.WithTimeout(ctx, streamCheckTimeout)
			valid := boundedStreamAuthority(check, entry)
			if valid {
				valid = browser.Ping(check) == nil && backend.Ping(check) == nil
			}
			done()
			if !valid {
				return
			}
		}
	}
}

// A provider Read has no context API. Keep transport closure bounded even if a
// provider stalls. The global cap also bounds checks from repeated reconnects.
func boundedStreamAuthority(ctx context.Context, entry *callbackAdmission) bool {
	select {
	case streamAuthorityChecks <- struct{}{}:
	default:
		return false
	}
	result := make(chan bool, 1)
	go func() {
		defer func() { <-streamAuthorityChecks }()
		result <- streamAuthorityCurrent(ctx, entry)
	}()
	select {
	case valid := <-result:
		return valid && ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

func streamAuthorityCurrent(ctx context.Context, entry *callbackAdmission) bool {
	user, status := resolveCurrentUser(ctx, entry)
	if status != http.StatusOK || user.IsRestricted || user.IsAdmin != entry.authority.Actor.SiteAdmin || user.Name != entry.authority.Actor.Username {
		return false
	}
	if expected := entry.authority.Repository; expected != nil {
		current, status := resolveRepository(ctx, entry, expected.ID)
		if status != http.StatusOK || current.Permission != expected.Permission {
			return false
		}
	}
	return ctx.Err() == nil
}
