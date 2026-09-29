// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCallbackResponseBoundsAndSingleObject(t *testing.T) {
	valid := `{"actor":{"id":"42","username":"soda-tester","site_admin":false}}`
	request := CallbackRequest{Operation: OperationCurrentActor}
	for name, body := range map[string]string{
		"oversized field":               `{"actor":{"id":"42","username":"` + strings.Repeat("x", maxCallbackResponseBytes) + `"}}`,
		"oversized trailing whitespace": valid + strings.Repeat(" ", maxCallbackResponseBytes-len(valid)+1),
		"second object":                 valid + `{}`,
		"second value":                  valid + `true`,
		"trailing bytes":                valid + `private-admission`,
		"truncated object":              valid[:len(valid)-1],
		"null":                          `null`,
		"array":                         `[]`,
		"unknown field":                 `{"private-admission":"secret"}`,
		"error text":                    `{"error_code":"` + strings.Repeat("private-admission", 100) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeCallbackResponse(strings.NewReader(body), request)
			if err == nil {
				t.Fatal("invalid callback response accepted")
			}
			if len(err.Error()) > 100 || strings.Contains(err.Error(), "private-admission") {
				t.Fatalf("callback error was not bounded and redacted: %q", err)
			}
		})
	}
	// The byte limit includes whitespace, and exactly the limit is valid.
	result, err := decodeCallbackResponse(strings.NewReader(valid+strings.Repeat(" ", maxCallbackResponseBytes-len(valid))), request)
	if err != nil || result.Actor == nil || result.Actor.ID != "42" {
		t.Fatalf("valid boundary response failed: %v", err)
	}
	if _, err := decodeCallbackResponse(failingCallbackReader{}, request); err == nil || strings.Contains(err.Error(), "private-admission") {
		t.Fatalf("reader error was not redacted: %v", err)
	}
}

type failingCallbackReader struct{}

func (failingCallbackReader) Read([]byte) (int, error) {
	return 0, errors.New("private-admission")
}

func TestCallbackRequiresOperationResult(t *testing.T) {
	for _, test := range []struct {
		request CallbackRequest
		valid   string
		invalid []string
	}{
		{CallbackRequest{Operation: OperationCurrentActor}, `{"actor":{"id":"42"}}`, []string{`{}`, `{"actor":null}`, `{"actor":{"id":"01"}}`, `{"owner":true}`, `{"actor":{"id":"42"},"owner":true}`}},
		{CallbackRequest{Operation: OperationRepository, RepositoryID: "7"}, `{"repository":{"id":"7","owner":"soda-tester","name":"project","permission":"read"}}`, []string{`{}`, `{"repository":{"id":"8","owner":"soda-tester","name":"project","permission":"read"}}`, `{"repository":{"id":"7","owner":"soda-tester","name":"project","permission":"none"}}`}},
		{CallbackRequest{Operation: OperationOwnedRepositories, Limit: 1}, `{"page":{"items":[],"next_cursor":""}}`, []string{`{}`, `{"page":{}}`, `{"page":{"items":[{"id":"01"}]}}`, `{"page":{"items":[{"id":"1"},{"id":"2"}]}}`}},
		{CallbackRequest{Operation: OperationOrganizationOwner}, `{"owner":false}`, []string{`{}`, `{"owner":null}`, `{"owner":"false"}`}},
		{CallbackRequest{Operation: OperationPublicSSHKeys}, `{"public_keys":[]}`, []string{`{}`, `{"public_keys":null}`, `{"public_keys":[{"id":"01","key":"public"}]}`, `{"public_keys":[{"id":"1"}]}`}},
	} {
		t.Run(test.request.Operation, func(t *testing.T) {
			if _, err := decodeCallbackResponse(strings.NewReader(test.valid), test.request); err != nil {
				t.Fatalf("expected result rejected: %v", err)
			}
			for _, body := range test.invalid {
				if _, err := decodeCallbackResponse(strings.NewReader(body), test.request); err == nil {
					t.Errorf("invalid result accepted: %s", body)
				}
			}
		})
	}
}

func TestNativeClientRejectsOversizedAndMultipleResponses(t *testing.T) {
	root := filepath.Join("..", ".artifacts", "tmp")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(root, "sdk-response-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	socket := filepath.Join(directory, "host.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"actor":{"id":"42"}}`
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := valid + `{}`
		if r.Header.Get(AdmissionHeader) == "oversized" {
			body = valid + strings.Repeat(" ", maxCallbackResponseBytes)
		}
		_, _ = io.WriteString(w, body)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	for _, admission := range []string{"oversized", "multiple"} {
		client := &nativeClient{socket: socket, admission: admission}
		if _, err := client.CurrentActor(context.Background()); err == nil {
			t.Errorf("%s response accepted through client", admission)
		}
	}
}

func TestAuthorityExcludesAdmissionAndRejectsNumericIDs(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(ContextHeader, `{"extension_id":"example","instance_id":"run-1","session_generation":"session-1","contribution":{"id":"notes","kind":"panel","scope":"user","action":"read"},"actor":{"id":"9007199254740993","username":"soda-tester","site_admin":false}}`)
	request.Header.Set(AdmissionHeader, "private-admission")
	authority, err := RequestContext(request)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-admission") || !strings.Contains(string(encoded), `"id":"9007199254740993"`) {
		t.Fatalf("unsafe authority JSON: %s", encoded)
	}
	request.Header.Set(ContextHeader, strings.Replace(request.Header.Get(ContextHeader), `"9007199254740993"`, `9007199254740993`, 1))
	if _, err := RequestContext(request); err == nil {
		t.Fatal("numeric actor id accepted")
	}
	request.Header.Set(ContextHeader, strings.Replace(request.Header.Get(ContextHeader), `9007199254740993`, `"01"`, 1))
	if _, err := RequestContext(request); err == nil {
		t.Fatal("noncanonical actor id accepted")
	}
}

func TestRequestContextUsesServiceCallbackSocket(t *testing.T) {
	t.Setenv(CallbackEnv, "/run/forgejo/private-instance.sock")
	t.Setenv(ServiceCallbackEnv, "/run/forgejo/service.sock")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(ContextHeader, `{"extension_id":"soda","instance_id":"run-1","session_generation":"session-1","contribution":{"id":"spaces","kind":"page","scope":"global","action":"get"},"actor":{"id":"1","username":"soda-tester","site_admin":false}}`)
	request.Header.Set(AdmissionHeader, "private-admission")
	authority, err := RequestContext(request)
	if err != nil {
		t.Fatal(err)
	}
	if authority.callbackSocket != "/run/forgejo/service.sock" {
		t.Fatalf("service callback socket = %q", authority.callbackSocket)
	}
}

func TestNativeClientUsesPrivateCallback(t *testing.T) {
	root := filepath.Join("..", ".artifacts", "tmp")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(root, "sdk-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	socket := filepath.Join(directory, "host.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != NativeCallbackPath || r.Method != http.MethodPost || r.Header.Get(AdmissionHeader) != "private-admission" {
			t.Errorf("unexpected callback request: %s %s admission=%q", r.Method, r.URL.Path, r.Header.Get(AdmissionHeader))
			http.Error(w, "bad callback", http.StatusBadRequest)
			return
		}
		var input CallbackRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		if input.Operation != OperationRepository || input.RepositoryID != "9007199254740993" {
			t.Errorf("unexpected callback input: %+v", input)
		}
		if input.Authority.ExtensionID != "example" || input.Authority.InstanceID != "run-1" || input.Authority.SessionGeneration != "session-1" || input.Authority.Contribution.Action != "read" || input.Authority.Actor.ID != "1" {
			t.Errorf("callback did not carry parsed authority claims: %+v", input.Authority)
		}
		_ = json.NewEncoder(w).Encode(CallbackResponse{Repository: &Repository{ID: input.RepositoryID, Owner: "soda-tester", Name: "project", Permission: "read"}})
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	t.Setenv(CallbackEnv, socket)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(ContextHeader, `{"extension_id":"example","instance_id":"run-1","session_generation":"session-1","contribution":{"id":"notes","kind":"panel","scope":"user","action":"read"},"actor":{"id":"1","username":"soda-tester","site_admin":false}}`)
	request.Header.Set(AdmissionHeader, "private-admission")
	authority, err := RequestContext(request)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := authority.Native().Repository(context.Background(), "9007199254740993")
	if err != nil || repository.ID != "9007199254740993" {
		t.Fatalf("repository callback: %+v, %v", repository, err)
	}
	if _, err := authority.Native().Repository(context.Background(), "01"); err == nil {
		t.Fatal("noncanonical repository id accepted")
	}
}

func TestManifestDeclarations(t *testing.T) {
	manifest := Manifest{Protocol: Protocol, ID: "example", Name: "Example", Version: "1", Executable: "backend", Capabilities: []string{CapabilityActorRead, CapabilityContributionAuthorize, CapabilityServiceBridge}, Policies: []string{PolicyForgejoUsername}, PreferredWorkspace: true}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	manifest.Capabilities = append(manifest.Capabilities, CapabilityActorRead)
	if err := manifest.Validate(); err == nil {
		t.Fatal("duplicate capability accepted")
	}
	manifest.Capabilities = []string{"native.anything"}
	if err := manifest.Validate(); err == nil {
		t.Fatal("unknown capability accepted")
	}
}
