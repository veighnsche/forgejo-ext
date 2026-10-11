// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestValidateOperationsHandshake(t *testing.T) {
	ok := OperationsHandshake{Installation: "install-1", Generation: "ops-abc", Kinds: []string{"issue.lifecycle.apply"}}
	if err := ValidateOperationsHandshake(ok); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	empty := OperationsHandshake{Installation: "install-1", Generation: "ops-abc"}
	if err := ValidateOperationsHandshake(empty); err != nil {
		t.Fatalf("empty kinds rejected: %v", err)
	}
	for i, h := range []OperationsHandshake{
		{},
		{Generation: "g"},
		{Installation: "i"},
		{Installation: "i", Generation: "g", Kinds: []string{""}},
		{Installation: "i", Generation: "g", Kinds: []string{"bad kind"}},
	} {
		if err := ValidateOperationsHandshake(h); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestReadOperationsRoundTrip(t *testing.T) {
	const admission = "0123456789abcdefghij0123456789abcdefghijk"
	socket := unixBackgroundServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != BackgroundOperationsPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(OperationsHandshake{Installation: "install-1", Generation: "ops-abc", Kinds: []string{"issue.lifecycle.apply", "pull_request.review.request"}})
	}))
	client := &backgroundClient{socket: socket, admission: admission, installation: "install-1"}
	handshake, err := client.ReadOperations(context.Background())
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if handshake.Generation != "ops-abc" || len(handshake.Kinds) != 2 {
		t.Fatalf("handshake: %+v", handshake)
	}
}

func TestReadOperationsRejectsForeignInstallation(t *testing.T) {
	const admission = "0123456789abcdefghij0123456789abcdefghijk"
	socket := unixBackgroundServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OperationsHandshake{Installation: "other-install", Generation: "ops-abc"})
	}))
	client := &backgroundClient{socket: socket, admission: admission, installation: "install-1"}
	if _, err := client.ReadOperations(context.Background()); err == nil {
		t.Fatal("foreign installation accepted")
	}
}
