// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	extension "forgejo.org/extension-sdk"
	extensions_service "forgejo.org/services/extensions"

	"github.com/stretchr/testify/require"
)

func controlPeerAddr(uid uint32) string {
	return extension.FormatUnixPeer(extension.UnixPeer{UID: uid, GID: uid, PID: 100})
}

// startControlManager starts one manager without required packages for
// transport-framing tests. Peer success paths need a required mapping
// and run unstarted below.
func startControlManager(t *testing.T, root string) *extensions_service.Manager {
	t.Helper()
	m := extensions_service.NewManager(root)
	require.NoError(t, m.Start(t.Context()))
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func serveControl(t *testing.T, manager *extensions_service.Manager, method, path, remoteAddr, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	handler := ManagerControlMux(manager, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = remoteAddr
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestManagerControlDelegatesUnknownPaths(t *testing.T) {
	root := adminTestRoot(t)
	m := startControlManager(t, root)
	recorder := serveControl(t, m, http.MethodGet, "/other", controlPeerAddr(2000), "", "")
	require.Equal(t, http.StatusTeapot, recorder.Code)
}

func TestManagerControlRejectsTransport(t *testing.T) {
	root := adminTestRoot(t)
	m := startControlManager(t, root)
	peer := controlPeerAddr(2000)

	recorder := serveControl(t, m, http.MethodGet, extensions_service.ManagerControlStatePath, peer, "application/json", `{"package_id":"sample"}`)
	require.Equal(t, http.StatusNotFound, recorder.Code)

	recorder = serveControl(t, m, http.MethodPost, extensions_service.ManagerControlStatePath+"?debug=1", peer, "application/json", `{"package_id":"sample"}`)
	require.Equal(t, http.StatusNotFound, recorder.Code)

	recorder = serveControl(t, m, http.MethodPost, extensions_service.ManagerControlStatePath, peer, "text/plain", `{"package_id":"sample"}`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	// Body decoding runs after peer authorization, so a malformed body
	// from an unauthorized peer is still forbidden here; the 400 path
	// is covered with an authorized peer below.
	recorder = serveControl(t, m, http.MethodPost, extensions_service.ManagerControlStatePath, "bogus-remote", "application/json", `{"package_id":"sample"}`)
	require.Equal(t, http.StatusForbidden, recorder.Code)

	recorder = serveControl(t, m, http.MethodPost, extensions_service.ManagerControlStatePath, peer, "application/json", `{"package_id":"sample"}`)
	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestManagerControlAuthorizedPeerFraming(t *testing.T) {
	root := adminTestRoot(t)
	m := extensions_service.NewManager(root, "soda")
	require.NoError(t, m.SetServiceBridgePeers(map[uint32]string{2000: "soda"}))
	peer := controlPeerAddr(2000)

	// The empty catalog controls nothing: the peer is authorized but
	// the package is outside its controlled set.
	recorder := serveControl(t, m, http.MethodPost, extensions_service.ManagerControlStatePath, peer, "application/json", `{"package_id":"sample"}`)
	require.Equal(t, http.StatusForbidden, recorder.Code)

	recorder = serveControl(t, m, http.MethodPost, extensions_service.ManagerControlSnapshotPath, peer, "application/json", `{}`)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)

	recorder = serveControl(t, m, http.MethodPost, extensions_service.ManagerControlReconcilePath, peer, "application/json", `{}`)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)

	oversize := strings.Repeat(" ", (2<<20)+1)
	recorder = serveControl(t, m, http.MethodPost, extensions_service.ManagerControlCachePath, peer, "application/json", oversize)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	recorder = serveControl(t, m, http.MethodPost, extensions_service.ManagerControlCachePath, peer, "application/json", `{"catalog":{}}`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	recorder = serveControl(t, m, http.MethodPost, extensions_service.ManagerControlStatePath, peer, "application/json", `{"package_id":`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestManagerControlNilManager(t *testing.T) {
	peer := controlPeerAddr(2000)
	for _, path := range []string{
		extensions_service.ManagerControlStatePath,
		extensions_service.ManagerControlSnapshotPath,
		extensions_service.ManagerControlReconcilePath,
		extensions_service.ManagerControlCachePath,
	} {
		recorder := serveControl(t, nil, http.MethodPost, path, peer, "application/json", `{}`)
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code, path)
	}
}
