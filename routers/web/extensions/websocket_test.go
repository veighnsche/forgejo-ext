// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"code.forgejo.org/go-chi/session"
	"forgejo.org/modules/setting"
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

func (s stalledStreamSession) Read(string) (session.RawStore, error) { <-s.release; return nil, nil }

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
