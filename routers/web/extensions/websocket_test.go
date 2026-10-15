// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/modules/setting"

	"code.forgejo.org/go-chi/session"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestStreamOriginAndHTTPDeadline(t *testing.T) {
	previous := setting.AppURL
	setting.AppURL = "https://forge.example/base/"
	defer func() { setting.AppURL = previous }()
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "https://forge.example/base/stream", nil)
		r.Header.Set("Origin", "https://forge.example")
		r.Header.Set("Upgrade", "websocket")
		return r
	}
	require.True(t, validStreamOrigin(request()))
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Origin") },
		func(r *http.Request) { r.Header.Add("Origin", "https://forge.example") },
		func(r *http.Request) { r.Header.Set("Origin", "https://other.example") },
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		func(r *http.Request) { r.Header.Set("Sec-WebSocket-Protocol", "credential") },
		func(r *http.Request) { r.URL.RawQuery = "generation=stale" },
		func(r *http.Request) { r.URL.ForceQuery = true },
		func(r *http.Request) { r.Method = http.MethodPost },
	} {
		r := request()
		mutate(r)
		require.False(t, validStreamOrigin(r))
	}
	ctx, cancel := extensionRequestContext(request())
	defer cancel()
	_, bounded := ctx.Deadline()
	require.False(t, bounded)
	ordinary := request()
	ordinary.Header.Del("Upgrade")
	ctx, cancel = extensionRequestContext(ordinary)
	defer cancel()
	deadline, bounded := ctx.Deadline()
	require.True(t, bounded)
	require.InDelta(t, 30, time.Until(deadline).Seconds(), 1)
}

type stalledStreamSession struct {
	session.Store
	release <-chan struct{}
}

//nolint:nilnil // The fixture deliberately supplies no session or backend.
func (s stalledStreamSession) Read(string) (session.RawStore, error) { <-s.release; return nil, nil }

//nolint:nilnil // The fixture deliberately supplies no session or backend.
func TestStreamRejectsUnscopedPathBeforeDial(t *testing.T) {
	previous := setting.AppURL
	setting.AppURL = "https://forge.example/base/"
	t.Cleanup(func() { setting.AppURL = previous })
	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	token, err := nativeAdmissions.issue(&callbackAdmission{instanceID: "stream-instance", requestCtx: requestContext, cancel: cancel})
	require.NoError(t, err)
	t.Cleanup(func() { nativeAdmissions.revoke(token) })
	req := httptest.NewRequest(http.MethodGet, "https://forge.example/base/stream", nil)
	req.Header.Set("Origin", "https://forge.example")
	req.Header.Set("Upgrade", "websocket")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", url.PathEscape("../host-secret"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	ctx, recorder := testContext(req)
	proxyWebSocket(ctx, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unscoped stream path must not reach the backend")
		return nil, nil
	}), extension.Authority{}, token)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

// websocketPair returns both ends of one loopback stream connection.
func websocketPair(t *testing.T) (client, server *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		accepted <- conn
	}))
	t.Cleanup(srv.Close)
	dialed, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dialed.CloseNow() })
	select {
	case server = <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("loopback stream was not accepted")
	}
	t.Cleanup(func() { _ = server.CloseNow() })
	return dialed, server
}

func TestRelayStreamForwardsAndRetiresOnCancellation(t *testing.T) {
	browserClient, browserPeer := websocketPair(t)
	backendClient, backendPeer := websocketPair(t)
	streamContext, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		relayStream(streamContext, browserClient, backendClient)
	}()
	require.NoError(t, browserPeer.Write(context.Background(), websocket.MessageText, []byte("ping")))
	kind, body, err := backendPeer.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, kind)
	require.Equal(t, "ping", string(body))
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream relay did not retire on cancellation")
	}
}

func TestStreamCheckTimeoutDoesNotWaitForSessionProvider(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	entry := &callbackAdmission{session: stalledStreamSession{release: release}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- boundedStreamAuthority(ctx, entry) }()
	select {
	case valid := <-done:
		require.False(t, valid)
	case <-time.After(time.Second):
		t.Fatal("stream cancellation waited for an uninterruptible provider")
	}
}
