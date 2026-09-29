// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forgejo.org/modules/extensions"
)

func notesRequest(t *testing.T, handler http.Handler, method string, actorID int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/notes", strings.NewReader(body))
	authority, err := json.Marshal(extensions.RequestAuthority{
		ExtensionID: "example",
		PageID:      "notes",
		Scope:       "panel",
		Actor:       extensions.Actor{ID: actorID, Username: "soda-tester"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(extensions.ContextHeader, string(authority))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestNotesAreIsolatedAndPersisted(t *testing.T) {
	dataDir := t.TempDir()
	handler := newHandler(dataDir)
	if response := notesRequest(t, handler, http.MethodPut, 1, "first actor note"); response.Code != http.StatusNoContent {
		t.Fatalf("save first note: %d", response.Code)
	}
	if response := notesRequest(t, handler, http.MethodGet, 2, ""); response.Code != http.StatusOK || response.Body.String() != "" {
		t.Fatalf("other actor saw note: %d %q", response.Code, response.Body.String())
	}
	if response := notesRequest(t, handler, http.MethodPut, 2, "second actor note"); response.Code != http.StatusNoContent {
		t.Fatalf("save second note: %d", response.Code)
	}
	if response := notesRequest(t, newHandler(dataDir), http.MethodGet, 1, ""); response.Code != http.StatusOK || response.Body.String() != "first actor note" {
		t.Fatalf("first note lost after handler restart: %d %q", response.Code, response.Body.String())
	}
	if response := notesRequest(t, handler, http.MethodPut, 1, strings.Repeat("x", maxNoteBytes+1)); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized note accepted: %d", response.Code)
	}
	if response := notesRequest(t, handler, http.MethodGet, 1, ""); response.Body.String() != "first actor note" {
		t.Fatalf("oversized write changed saved note: %q", response.Body.String())
	}
	if response := notesRequest(t, handler, http.MethodGet, 0, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing actor accepted: %d", response.Code)
	}
}
