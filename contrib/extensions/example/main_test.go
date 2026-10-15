// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json" //nolint:depguard // The independently built example imports only the public SDK.
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	extensions "forgejo.org/extension-sdk"

	"github.com/coder/websocket"
)

func notesRequest(t *testing.T, handler http.Handler, method, actorID, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/notes", strings.NewReader(body))
	authority, err := json.Marshal(extensions.Authority{
		ExtensionID: "example", InstanceID: "instance", SessionGeneration: "generation",
		Contribution: extensions.Contribution{ID: "notes", Kind: "panel", Scope: "user", Action: "read"},
		Actor:        extensions.Actor{ID: actorID, Username: "soda-tester"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(extensions.ContextHeader, string(authority))
	request.Header.Set(extensions.AdmissionHeader, "opaque-test-admission")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestNotesAreIsolatedAndPersisted(t *testing.T) {
	dataDir := t.TempDir()
	handler := newHandler(dataDir)
	if response := notesRequest(t, handler, http.MethodPut, "1", "first actor note"); response.Code != http.StatusNoContent {
		t.Fatalf("save first note: %d", response.Code)
	}
	if response := notesRequest(t, handler, http.MethodGet, "2", ""); response.Code != http.StatusOK || response.Body.String() != "" {
		t.Fatalf("other actor saw note: %d %q", response.Code, response.Body.String())
	}
	if response := notesRequest(t, handler, http.MethodPut, "2", "second actor note"); response.Code != http.StatusNoContent {
		t.Fatalf("save second note: %d", response.Code)
	}
	if response := notesRequest(t, newHandler(dataDir), http.MethodGet, "1", ""); response.Code != http.StatusOK || response.Body.String() != "first actor note" {
		t.Fatalf("first note lost after handler restart: %d %q", response.Code, response.Body.String())
	}
	if response := notesRequest(t, handler, http.MethodPut, "1", strings.Repeat("x", maxNoteBytes+1)); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized note accepted: %d", response.Code)
	}
	if response := notesRequest(t, handler, http.MethodGet, "1", ""); response.Body.String() != "first actor note" {
		t.Fatalf("oversized write changed saved note: %q", response.Body.String())
	}
	if response := notesRequest(t, handler, http.MethodGet, "0", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing actor accepted: %d", response.Code)
	}
}

func streamAuthority(t *testing.T, actorID string) (string, string) {
	t.Helper()
	authority, err := json.Marshal(extensions.Authority{
		ExtensionID: "example", InstanceID: "instance", SessionGeneration: "generation",
		Contribution: extensions.Contribution{ID: "notes", Kind: "panel", Scope: "user", Action: "read"},
		Actor:        extensions.Actor{ID: actorID, Username: "soda-tester"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(authority), "opaque-test-admission"
}

func TestStreamRejectsMissingAuthorityAndLeakedCredentials(t *testing.T) {
	handler := newHandler(t.TempDir())
	plain := httptest.NewRequest(http.MethodGet, "/stream", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, plain)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing authority accepted: %d", response.Code)
	}
	contextHeader, admission := streamAuthority(t, "7")
	for name, header := range map[string]string{
		"cookie":             "Cookie",
		"authorization":      "Authorization",
		"websocket-protocol": "Sec-WebSocket-Protocol",
		"session-generation": extensions.SessionGenerationHeader,
	} {
		request := httptest.NewRequest(http.MethodGet, "/stream", nil)
		request.Header.Set(extensions.ContextHeader, contextHeader)
		request.Header.Set(extensions.AdmissionHeader, admission)
		request.Header.Set(header, "leaked")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s credential accepted: %d", name, response.Code)
		}
	}
	// Valid authority without a native callback host cannot open the socket.
	request := httptest.NewRequest(http.MethodGet, "/stream", nil)
	request.Header.Set(extensions.ContextHeader, contextHeader)
	request.Header.Set(extensions.AdmissionHeader, admission)
	t.Setenv(extensions.CallbackEnv, filepath.Join(t.TempDir(), "absent.sock"))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing native callback accepted: %d", response.Code)
	}
}

func TestStreamGreetsAndEchoesAdmittedPanel(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "callback.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := http.NewServeMux()
	callbacks.HandleFunc(extensions.NativeCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		var callback extensions.CallbackRequest
		if err := json.NewDecoder(r.Body).Decode(&callback); err != nil || callback.Operation != extensions.OperationCurrentActor {
			http.Error(w, "unexpected callback", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(extensions.CallbackResponse{Actor: &extensions.Actor{ID: "7", Username: "soda-tester"}})
	})
	server := &http.Server{Handler: callbacks}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv(extensions.CallbackEnv, socket)

	backend := httptest.NewServer(newHandler(t.TempDir()))
	t.Cleanup(backend.Close)
	contextHeader, admission := streamAuthority(t, "7")
	dialHeader := http.Header{}
	dialHeader.Set(extensions.ContextHeader, contextHeader)
	dialHeader.Set(extensions.AdmissionHeader, admission)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(backend.URL, "http")+"/stream", &websocket.DialOptions{HTTPHeader: dialHeader})
	if err != nil {
		t.Fatalf("dial admitted stream: %v", err)
	}
	defer conn.CloseNow()
	_, hello, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	var greeting map[string]string
	if err := json.Unmarshal(hello, &greeting); err != nil || greeting["hello"] != "soda-tester" {
		t.Fatalf("unexpected greeting: %q", hello)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"ping":"ping-1"}`)); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	_, echo, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	var reply map[string]string
	if err := json.Unmarshal(echo, &reply); err != nil || reply["ping"] != "ping-1" {
		t.Fatalf("unexpected echo: %q", echo)
	}
}
