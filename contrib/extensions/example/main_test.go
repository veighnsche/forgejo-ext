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
	return notesContributionRequest(t, handler, method, actorID, body, extensions.Contribution{ID: "notes", Kind: "panel", Scope: "panel", Action: "read"})
}

func notesContributionRequest(t *testing.T, handler http.Handler, method, actorID, body string, contribution extensions.Contribution) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/notes", strings.NewReader(body))
	contextHeader, admission := contributionAuthority(t, actorID, contribution)
	request.Header.Set(extensions.ContextHeader, contextHeader)
	request.Header.Set(extensions.AdmissionHeader, admission)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func contributionAuthority(t *testing.T, actorID string, contribution extensions.Contribution) (string, string) {
	t.Helper()
	authority, err := json.Marshal(extensions.Authority{
		ExtensionID: "example", InstanceID: "instance", SessionGeneration: "generation",
		Contribution: contribution,
		Actor:        extensions.Actor{ID: actorID, Username: "soda-tester"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(authority), "opaque-test-admission"
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
	return contributionAuthority(t, actorID, extensions.Contribution{ID: "notes", Kind: "panel", Scope: "panel", Action: "read"})
}

func TestNotesAcceptCanonicalWorkspaceAndRejectOtherContributions(t *testing.T) {
	handler := newHandler(t.TempDir())
	workspace := extensions.Contribution{ID: "home", Kind: "page", Scope: "global", Action: "read"}
	if response := notesContributionRequest(t, handler, http.MethodPut, "7", "shared workspace note", workspace); response.Code != http.StatusNoContent {
		t.Fatalf("save workspace note: %d", response.Code)
	}
	if response := notesRequest(t, handler, http.MethodGet, "7", ""); response.Code != http.StatusOK || response.Body.String() != "shared workspace note" {
		t.Fatalf("linked panel did not share workspace note: %d %q", response.Code, response.Body.String())
	}
	for _, contribution := range []extensions.Contribution{
		{ID: "repository", Kind: "page", Scope: "repository", Action: "read"},
		{ID: "home", Kind: "page", Scope: "repository", Action: "read"},
		{ID: "preferences", Kind: "page", Scope: "user", Action: "read"},
		{ID: "notes", Kind: "page", Scope: "global", Action: "read"},
		{ID: "home", Kind: "panel", Scope: "user", Action: "read"},
		{ID: "notes", Kind: "panel", Scope: "global", Action: "read"},
		{ID: "notes", Kind: "panel", Scope: "user", Action: "read"},
	} {
		t.Run(contribution.Kind+"/"+contribution.ID+"/"+contribution.Scope, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				if response := notesContributionRequest(t, handler, method, "7", "unauthorized overwrite", contribution); response.Code != http.StatusUnauthorized {
					t.Fatalf("unrelated contribution accepted for %s: %d", method, response.Code)
				}
			}
			contextHeader, admission := contributionAuthority(t, "7", contribution)
			request := httptest.NewRequest(http.MethodGet, "/stream", nil)
			request.Header.Set(extensions.ContextHeader, contextHeader)
			request.Header.Set(extensions.AdmissionHeader, admission)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("unrelated stream contribution accepted: %d", response.Code)
			}
		})
	}
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

func TestStreamGreetsAndEchoesAdmittedNotes(t *testing.T) {
	for _, contribution := range []extensions.Contribution{
		{ID: "notes", Kind: "panel", Scope: "panel", Action: "read"},
		{ID: "home", Kind: "page", Scope: "global", Action: "read"},
	} {
		t.Run(contribution.Kind, func(t *testing.T) {
			testStreamGreetsAndEchoes(t, contribution)
		})
	}
}

func testStreamGreetsAndEchoes(t *testing.T, contribution extensions.Contribution) {
	t.Helper()
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
	contextHeader, admission := contributionAuthority(t, "7", contribution)
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
