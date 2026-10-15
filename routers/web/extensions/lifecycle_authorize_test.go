// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	extension "forgejo.org/extension-sdk"
	runtime "forgejo.org/services/extensions"
)

// The authorize callback rides the existing per-instance and service
// multiplexers: unknown tokens, malformed bodies, query strings, and
// foreign instances fail closed before any manager state is touched.
func TestLifecycleAuthorizeMapping(t *testing.T) {
	manager := runtime.NewManager(t.TempDir())
	mux := BackgroundMux(manager, http.NotFoundHandler(), "instance-x", false)
	call := `{"controller_id":"controller","controller_instance_id":"instance-x","package_id":"sample","package_version":"1.0.0","package_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","installation_id":"550e8400-e29b-41d4-a716-446655440000","generation":"1","operation":"observe","deadline_unix":1792100010}`

	unknown := httptest.NewRequest(http.MethodPost, extension.LifecycleAuthorizePath, strings.NewReader(call))
	unknown.Header.Set("Content-Type", "application/json")
	unknown.Header.Set(extension.LifecycleAdmissionHeader, "bogus")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, unknown)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unknown token mapped to %d", response.Code)
	}

	malformed := httptest.NewRequest(http.MethodPost, extension.LifecycleAuthorizePath, strings.NewReader(`{"controller_id":`))
	malformed.Header.Set("Content-Type", "application/json")
	malformed.Header.Set(extension.LifecycleAdmissionHeader, "bogus")
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, malformed)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed call mapped to %d", response.Code)
	}

	queried := httptest.NewRequest(http.MethodPost, extension.LifecycleAuthorizePath+"?debug=1", strings.NewReader(call))
	queried.Header.Set("Content-Type", "application/json")
	queried.Header.Set(extension.LifecycleAdmissionHeader, "bogus")
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, queried)
	if response.Code != http.StatusNotFound {
		t.Fatalf("query string mapped to %d", response.Code)
	}

	foreign := httptest.NewRequest(http.MethodPost, extension.LifecycleAuthorizePath, strings.NewReader(strings.Replace(call, "instance-x", "instance-y", 1)))
	foreign.Header.Set("Content-Type", "application/json")
	foreign.Header.Set(extension.LifecycleAdmissionHeader, "bogus")
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, foreign)
	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign instance mapped to %d", response.Code)
	}
}
