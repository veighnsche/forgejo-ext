// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// frozenCall returns a canonical lifecycle call: the §5 identity tuple with
// the fixture clock's deadline one step ahead.
func frozenCall() LifecycleCall {
	return LifecycleCall{
		ControllerID:         "controller",
		ControllerInstanceID: "dddddddddddddddddddddddddddddddd",
		PackageID:            "sample",
		PackageVersion:       "1.0.0",
		PackageSHA256:        strings.Repeat("b", 64),
		InstallationID:       "550e8400-e29b-41d4-a716-446655440000",
		Generation:           "7",
		Operation:            LifecycleObserve,
		DeadlineUnix:         1792100010,
	}
}

const frozenClockUnix = 1792100000

func mustEncodeLifecycle(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDecodeLifecycleCallStrict(t *testing.T) {
	call, err := DecodeLifecycleCall(mustEncodeLifecycle(t, frozenCall()))
	if err != nil {
		t.Fatalf("valid call rejected: %v", err)
	}
	if err := ValidateLifecycleCall(call, frozenClockUnix); err != nil {
		t.Fatalf("valid call failed validation: %v", err)
	}
	if call != frozenCall() {
		t.Fatalf("call round trip changed the tuple: %+v", call)
	}
}

func TestDecodeLifecycleCallRejectsWireViolations(t *testing.T) {
	base := mustEncodeLifecycle(t, frozenCall())
	cases := map[string]func([]byte) []byte{
		"stray field": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"deadline_unix":`, `"pin":"x","deadline_unix":`, 1))
		},
		"duplicate key": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"operation":`, `"operation":"observe","operation":`, 1))
		},
		"trailing data": func(raw []byte) []byte {
			return append(append([]byte(nil), raw...), []byte(" {}")...)
		},
		"string deadline": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"deadline_unix":1792100010`, `"deadline_unix":"1792100010"`, 1))
		},
		"missing operation": func(raw []byte) []byte {
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			delete(fields, "operation")
			out, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			return out
		},
		"missing deadline": func(raw []byte) []byte {
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			delete(fields, "deadline_unix")
			out, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			return out
		},
		"empty object": func(_ []byte) []byte { return []byte(`{}`) },
		"array body":   func(_ []byte) []byte { return []byte(`[]`) },
		"oversize": func(_ []byte) []byte {
			padding := strings.Repeat("a", MaximumLifecycleBodyBytes)
			return []byte(`{"controller_id":"` + padding + `"}`)
		},
	}
	for name, mutate := range cases {
		if _, err := DecodeLifecycleCall(mutate(base)); err == nil {
			t.Fatalf("wire violation accepted (%s)", name)
		}
	}
}

func TestValidateLifecycleCallIdentities(t *testing.T) {
	call := frozenCall()
	cases := map[string]func(*LifecycleCall){
		"bad controller":  func(c *LifecycleCall) { c.ControllerID = "../x" },
		"long controller": func(c *LifecycleCall) { c.ControllerID = strings.Repeat("a", 65) },
		"empty instance":  func(c *LifecycleCall) { c.ControllerInstanceID = "" },
		"long instance":   func(c *LifecycleCall) { c.ControllerInstanceID = strings.Repeat("a", 129) },
		"bad package":     func(c *LifecycleCall) { c.PackageID = "Tailnet" },
		"bad version":     func(c *LifecycleCall) { c.PackageVersion = "v 1" },
		"bad digest":      func(c *LifecycleCall) { c.PackageSHA256 = strings.Repeat("B", 64) },
		"bad install":     func(c *LifecycleCall) { c.InstallationID = "not-a-uuid" },
		"short install":   func(c *LifecycleCall) { c.InstallationID = "550e8400-e29b-41d4-a716-44665544" },
		"padded gen":      func(c *LifecycleCall) { c.Generation = "07" },
		"empty gen":       func(c *LifecycleCall) { c.Generation = "" },
		"huge gen":        func(c *LifecycleCall) { c.Generation = "18446744073709551616" },
		"unknown op":      func(c *LifecycleCall) { c.Operation = "restart" },
		"expired":         func(c *LifecycleCall) { c.DeadlineUnix = frozenClockUnix },
		"zero deadline":   func(c *LifecycleCall) { c.DeadlineUnix = 0 },
	}
	for name, mutate := range cases {
		bad := call
		mutate(&bad)
		if err := ValidateLifecycleCall(bad, frozenClockUnix); err == nil {
			t.Fatalf("invalid call accepted (%s)", name)
		}
	}
	// Boundary generations stay valid: virtual "0" and the uint64 maximum.
	for _, generation := range []string{"0", "1", "18446744073709551615"} {
		ok := call
		ok.Generation = generation
		if err := ValidateLifecycleCall(ok, frozenClockUnix); err != nil {
			t.Fatalf("canonical generation %q rejected: %v", generation, err)
		}
	}
}

func frozenObservation(call LifecycleCall) LifecycleObservation {
	effect := "6"
	return LifecycleObservation{
		LifecycleCall:    call,
		State:            LifecycleStateRunning,
		EffectGeneration: &effect,
		AdmissionReady:   true,
		Detail:           "Owned runtime is running with a current operational grant.",
	}
}

func TestValidateLifecycleObservationRules(t *testing.T) {
	call := frozenCall()
	good := frozenObservation(call)
	if _, err := DecodeLifecycleObservation(mustEncodeLifecycle(t, good)); err != nil {
		t.Fatalf("valid observation rejected by strict decode: %v", err)
	}
	if err := ValidateLifecycleObservation(good, call, frozenClockUnix); err != nil {
		t.Fatalf("valid observation rejected: %v", err)
	}
	if !good.Echoes(call) {
		t.Fatal("valid observation does not echo its call")
	}
	// A prior recorded effect generation is valid cleanup and adoption
	// evidence: the wire must never require it to equal the request
	// generation, or post-replacement cleanup would be impossible.
	older := "5"
	adopted := good
	adopted.EffectGeneration = &older
	if err := ValidateLifecycleObservation(adopted, call, frozenClockUnix); err != nil {
		t.Fatalf("prior effect generation rejected: %v", err)
	}
	// Running with a lost grant is factual but not ready: it validates
	// while reporting no admission.
	ungranted := good
	ungranted.AdmissionReady = false
	if err := ValidateLifecycleObservation(ungranted, call, frozenClockUnix); err != nil {
		t.Fatalf("running-but-ungranted observation rejected: %v", err)
	}
	if ungranted.AdmissionReady {
		t.Fatal("running-but-ungranted observation reports ready")
	}
	tampered := call
	tampered.DeadlineUnix++
	if ValidateLifecycleObservation(good, tampered, frozenClockUnix) == nil {
		t.Fatal("observation with a mismatched echoed tuple accepted")
	}
	if good.Echoes(tampered) {
		t.Fatal("mismatched tuple echoes")
	}
	cases := map[string]func(*LifecycleObservation){
		"unknown state":          func(o *LifecycleObservation) { o.State = "enabled" },
		"running without effect": func(o *LifecycleObservation) { o.EffectGeneration = nil },
		"stopped with effect": func(o *LifecycleObservation) {
			o.State = LifecycleStateStopped
		},
		"unknown with effect": func(o *LifecycleObservation) {
			o.State = LifecycleStateUnknown
		},
		"stopped with admission": func(o *LifecycleObservation) {
			o.State = LifecycleStateStopped
			o.EffectGeneration = nil
			o.AdmissionReady = true
		},
		"error with admission": func(o *LifecycleObservation) {
			o.Error = &LifecycleError{Code: LifecycleErrUnconfirmed, Message: "effect unknown"}
		},
		"bad error code": func(o *LifecycleObservation) {
			o.AdmissionReady = false
			o.Error = &LifecycleError{Code: "teardown_failed", Message: "old code"}
		},
		"empty error message": func(o *LifecycleObservation) {
			o.AdmissionReady = false
			o.Error = &LifecycleError{Code: LifecycleErrEffectFailed, Message: "  "}
		},
		"long detail": func(o *LifecycleObservation) {
			o.Detail = strings.Repeat("d", MaximumLifecycleDetailBytes+1)
		},
	}
	for name, mutate := range cases {
		bad := good
		effect := "6"
		bad.EffectGeneration = &effect
		mutate(&bad)
		if err := ValidateLifecycleObservation(bad, call, frozenClockUnix); err == nil {
			t.Fatalf("invalid observation accepted (%s)", name)
		}
	}
	// A confirmed stop carries a null effect and no grant.
	stopped := LifecycleObservation{
		LifecycleCall: call, State: LifecycleStateStopped, AdmissionReady: false,
		Detail: "Owned runtime stopped; operational admission is closed.",
	}
	if _, err := DecodeLifecycleObservation(mustEncodeLifecycle(t, stopped)); err != nil {
		t.Fatalf("stopped observation rejected by strict decode: %v", err)
	}
	if err := ValidateLifecycleObservation(stopped, call, frozenClockUnix); err != nil {
		t.Fatalf("stopped observation rejected: %v", err)
	}
}

func TestDecodeLifecycleObservationRequiresPresence(t *testing.T) {
	good := mustEncodeLifecycle(t, frozenObservation(frozenCall()))
	var fields map[string]any
	if err := json.Unmarshal(good, &fields); err != nil {
		t.Fatal(err)
	}
	// An omitted admission_ready must never decode as an explicit false.
	delete(fields, "admission_ready")
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeLifecycleObservation(raw); err == nil {
		t.Fatal("omitted admission_ready accepted")
	}
	if err := json.Unmarshal(good, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "detail")
	if raw, err := json.Marshal(fields); err != nil {
		t.Fatal(err)
	} else if _, err := DecodeLifecycleObservation(raw); err == nil {
		t.Fatal("omitted detail accepted")
	}
	// A wrong scalar type fails instead of coercing.
	if raw := []byte(strings.Replace(string(good), `"admission_ready":true`, `"admission_ready":"true"`, 1)); func() bool {
		_, err := DecodeLifecycleObservation(raw)
		return err == nil
	}() {
		t.Fatal("string admission_ready accepted")
	}
}

func TestValidateLifecycleRegistration(t *testing.T) {
	manifest := Manifest{Protocol: Protocol, ID: "controller", Name: "C", Version: "1.0.0",
		Executable: "backend", Capabilities: []string{CapabilityExtensionLifecycle}}
	registered := Registration{AuthorizesContribution: false, Lifecycle: true}
	if err := ValidateLifecycleRegistration(manifest, registered); err != nil {
		t.Fatalf("matching registration rejected: %v", err)
	}
	// A missing controller registration fails the handshake: the declared
	// capability never becomes ready on an implicit no-op hook.
	if err := ValidateLifecycleRegistration(manifest, Registration{}); err == nil {
		t.Fatal("missing lifecycle registration accepted")
	}
	plain := Manifest{Protocol: Protocol, ID: "sample", Name: "S", Version: "1.0.0", Executable: "backend"}
	if err := ValidateLifecycleRegistration(plain, registered); err == nil {
		t.Fatal("undeclared lifecycle registration accepted")
	}
	if err := ValidateLifecycleRegistration(plain, Registration{}); err != nil {
		t.Fatalf("ordinary package without lifecycle rejected: %v", err)
	}
	if got := (Application{Lifecycle: nil}).registration(); got.Lifecycle {
		t.Fatal("nil handler registers lifecycle")
	}
	handler := func(_ context.Context, _ LifecycleAuthority, call LifecycleCall) (LifecycleObservation, error) {
		observation := frozenObservation(call)
		observation.Operation = call.Operation
		return observation, nil
	}
	if got := (Application{Lifecycle: handler}).registration(); !got.Lifecycle {
		t.Fatal("lifecycle handler did not register")
	}
}

func TestLifecycleAuthorityIsDistinctFromActor(t *testing.T) {
	t.Setenv(CallbackEnv, filepath.Join(t.TempDir(), "callback.sock"))
	t.Setenv(ServiceCallbackEnv, "")
	actorBody := `{"extension_id":"sample","instance_id":"i","session_generation":"1",` +
		`"contribution":{"id":"c","kind":"k","scope":"s","action":"a"},"actor":{"id":"1","username":"u","site_admin":true}}`
	actorRequest := httptest.NewRequest(http.MethodPost, "/", nil)
	actorRequest.Header.Set(ContextHeader, actorBody)
	actorRequest.Header.Set(AdmissionHeader, "ordinary-token")
	if _, err := LifecycleContext(actorRequest); err == nil {
		t.Fatal("ordinary actor admission accepted as lifecycle authority")
	}
	lifecycleRequest := httptest.NewRequest(http.MethodPost, LifecyclePath, nil)
	lifecycleRequest.Header.Set(LifecycleAdmissionHeader, "lifecycle-token")
	if _, err := RequestContext(lifecycleRequest); err == nil {
		t.Fatal("lifecycle token accepted as ordinary actor admission")
	}
	missing := httptest.NewRequest(http.MethodPost, LifecyclePath, nil)
	if _, err := LifecycleContext(missing); err == nil {
		t.Fatal("missing lifecycle admission accepted")
	}
	authority, err := LifecycleContext(lifecycleRequest)
	if err != nil {
		t.Fatalf("admitted lifecycle authority rejected: %v", err)
	}
	encoded, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "{}" || strings.Contains(string(encoded), "lifecycle-token") {
		t.Fatalf("lifecycle authority serializes its secret: %s", encoded)
	}
}

func TestLifecycleEndpointDispatch(t *testing.T) {
	t.Setenv(CallbackEnv, filepath.Join(t.TempDir(), "callback.sock"))
	t.Setenv(ServiceCallbackEnv, "")
	var observed LifecycleAuthority
	var observedCall LifecycleCall
	app := Application{
		HTTP: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "ordinary route reached", http.StatusTeapot)
		}),
		Lifecycle: func(_ context.Context, authority LifecycleAuthority, call LifecycleCall) (LifecycleObservation, error) {
			observed = authority
			observedCall = call
			return frozenObservation(call), nil
		},
	}
	call := frozenCall()
	request := httptest.NewRequest(http.MethodPost, LifecyclePath, bytes.NewReader(mustEncodeLifecycle(t, call)))
	request.Header.Set(LifecycleAdmissionHeader, "opaque-token")
	recorder := httptest.NewRecorder()
	app.handler().ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("lifecycle endpoint returned %d", response.StatusCode)
	}
	if observedCall != call {
		t.Fatalf("handler did not receive the call: %+v", observedCall)
	}
	if _, err := json.Marshal(observed); err != nil {
		t.Fatal(err)
	}
	var observation LifecycleObservation
	if err := json.NewDecoder(response.Body).Decode(&observation); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLifecycleObservation(observation, call, frozenClockUnix); err != nil {
		t.Fatalf("endpoint observation invalid: %v", err)
	}
	// Without a handler the endpoint is absent: ordinary routes must not
	// absorb lifecycle calls.
	plain := Application{HTTP: app.HTTP}
	bad := httptest.NewRequest(http.MethodPost, LifecyclePath, bytes.NewReader(mustEncodeLifecycle(t, call)))
	bad.Header.Set(LifecycleAdmissionHeader, "opaque-token")
	recorder = httptest.NewRecorder()
	plain.handler().ServeHTTP(recorder, bad)
	if recorder.Result().StatusCode != http.StatusNotFound {
		t.Fatal("lifecycle call reached ordinary dispatch without a handler")
	}
	// Query parameters and malformed bodies are rejected before dispatch.
	queried := httptest.NewRequest(http.MethodPost, LifecyclePath+"?gen=8", bytes.NewReader(mustEncodeLifecycle(t, call)))
	queried.Header.Set(LifecycleAdmissionHeader, "opaque-token")
	recorder = httptest.NewRecorder()
	app.handler().ServeHTTP(recorder, queried)
	if recorder.Result().StatusCode != http.StatusBadRequest {
		t.Fatal("lifecycle call with query parameters accepted")
	}
}

// authorizeFixture is a fake manager authorize callback for SDK wire
// tests. It admits exactly one call tuple with its desired flag at an
// injected clock, playing the manager half of the contract: strict body
// decode, token binding, deadline/identity check, complete tuple equality,
// and revocation after the command completes. Handler assertions use
// t.Errorf, which is safe from the serving goroutine.
type authorizeFixture struct {
	t        *testing.T
	token    string
	admitted LifecycleCall
	desired  bool
	nowUnix  int64
	revoked  atomic.Bool
}

func (f *authorizeFixture) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != LifecycleAuthorizePath {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get(LifecycleAdmissionHeader) != f.token {
			http.Error(w, "unauthorized", http.StatusForbidden)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, MaximumLifecycleBodyBytes+1))
		if err != nil || len(body) > MaximumLifecycleBodyBytes {
			http.Error(w, "oversized", http.StatusBadRequest)
			return
		}
		if bytes.Contains(body, []byte(f.token)) {
			f.t.Errorf("authorize request serializes its token")
		}
		if f.revoked.Load() {
			http.Error(w, "revoked", http.StatusForbidden)
			return
		}
		call, err := DecodeLifecycleCall(body)
		if err != nil {
			http.Error(w, "malformed", http.StatusBadRequest)
			return
		}
		if err := ValidateLifecycleCall(call, f.nowUnix); err != nil || call != f.admitted {
			http.Error(w, "mismatched", http.StatusForbidden)
			return
		}
		writeJSON(w, LifecycleVerification{Call: call, DesiredEnabled: f.desired})
	})
}

func serveAuthorizeFixture(t *testing.T, fixture *authorizeFixture) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "authorize.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: fixture.handler()}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return socket
}

func TestLifecycleVerifyConfinesCleanupAuthority(t *testing.T) {
	cleanup := frozenCall()
	cleanup.Operation = LifecycleDeactivate
	cleanup.Generation = "8"
	cleanup.DeadlineUnix = 1792100120
	fixture := &authorizeFixture{t: t, token: "cleanup-token", admitted: cleanup, desired: true, nowUnix: frozenClockUnix}
	socket := serveAuthorizeFixture(t, fixture)
	t.Setenv(CallbackEnv, socket)
	t.Setenv(ServiceCallbackEnv, "")
	authority := LifecycleAuthority{admission: "cleanup-token", callbackSocket: socket}
	verification, err := authority.Verify(context.Background(), cleanup)
	if err != nil {
		t.Fatalf("cleanup authority rejected: %v", err)
	}
	if verification.Call != cleanup || !verification.DesiredEnabled {
		t.Fatalf("cleanup verification wrong: %+v", verification)
	}
	// The cleanup admission authorizes only its deactivate tuple: the
	// same admission presented for activate fails. There is no
	// client-controlled cleanup flag; the operation is bound by the
	// issued tuple.
	activate := cleanup
	activate.Operation = LifecycleActivate
	if _, err := authority.Verify(context.Background(), activate); err == nil {
		t.Fatal("cleanup authority authorized activate")
	}
	// After the command completes the manager revokes the token and the
	// same authority fails: cleanup power cannot survive completion.
	fixture.revoked.Store(true)
	if _, err := authority.Verify(context.Background(), cleanup); err == nil {
		t.Fatal("revoked cleanup authority still verifies")
	}
	// An expired deadline fails even before revocation.
	fixture.revoked.Store(false)
	fixture.nowUnix = cleanup.DeadlineUnix
	if _, err := authority.Verify(context.Background(), cleanup); err == nil {
		t.Fatal("expired cleanup authority still verifies")
	}
	if _, err := (LifecycleAuthority{}).Verify(context.Background(), cleanup); err == nil {
		t.Fatal("unadmitted authority verifies")
	}
}

// sharedContractExamples is the frozen cross-language wire fixture source:
// sdk/testdata/rcx2-contract-examples.json, a byte-identical repository copy
// of the frozen contract-examples fixture (sha256
// 83208f74907b847ad489722513a15be419a6ebad664daa11bf89c410a0deaad1).
// Synthetic digests, URLs, versions and identities are test data, never
// artifact-verification evidence; the clock is injected for deadline
// validation.
const sharedContractExamples = "testdata/rcx2-contract-examples.json"

// TestFrozenContractExamplesWire is the F01 wire fixture: it decodes the
// shared frozen contract-examples.json with its injected clock and proves
// exact field/identity behavior, including the failed-enable cleanup
// authority confined to its active command.
func TestFrozenContractExamplesWire(t *testing.T) {
	data, err := os.ReadFile(sharedContractExamples)
	if err != nil {
		t.Fatalf("shared contract examples unavailable: %v", err)
	}
	var envelope struct {
		FixtureOnly          bool            `json:"fixture_only"`
		Clock                int64           `json:"fixture_clock_unix"`
		Catalog              json.RawMessage `json:"catalog"`
		Call                 json.RawMessage `json:"lifecycle_call"`
		Observation          json.RawMessage `json:"lifecycle_observation"`
		Authorization        json.RawMessage `json:"lifecycle_authorization"`
		ActivationInProgress struct {
			Call        json.RawMessage `json:"lifecycle_call"`
			Observation json.RawMessage `json:"lifecycle_observation"`
		} `json:"activation_in_progress"`
		DisabledAfterCleanup struct {
			Call        json.RawMessage `json:"lifecycle_call"`
			Observation json.RawMessage `json:"lifecycle_observation"`
		} `json:"disabled_after_cleanup"`
		FailedEnableCleanup struct {
			Call          json.RawMessage `json:"lifecycle_call"`
			Authorization json.RawMessage `json:"lifecycle_authorization"`
			Observation   json.RawMessage `json:"lifecycle_observation"`
		} `json:"failed_enable_cleanup"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.FixtureOnly {
		t.Fatal("shared fixtures must stay marked fixture-only")
	}
	if envelope.Clock != frozenClockUnix {
		t.Fatalf("fixture clock moved: %d", envelope.Clock)
	}
	clock := envelope.Clock
	catalog, err := DecodeCatalog(envelope.Catalog)
	if err != nil {
		t.Fatalf("fixture catalog rejected: %v", err)
	}
	tailnet, ok := FindCatalogPackage(catalog, "tailnet")
	if !ok || tailnet.LifecycleController != "soda" {
		t.Fatalf("fixture controller relation lost: %+v", tailnet)
	}
	for _, entry := range catalog.Packages {
		for _, capability := range entry.Capabilities {
			if strings.HasPrefix(capability, "tailnet.") {
				t.Fatalf("product capability on the generic wire: %q", capability)
			}
		}
	}
	call, err := DecodeLifecycleCall(envelope.Call)
	if err != nil {
		t.Fatalf("fixture call rejected: %v", err)
	}
	if err := ValidateLifecycleCall(call, clock); err != nil {
		t.Fatalf("fixture call invalid at its clock: %v", err)
	}
	observation, err := DecodeLifecycleObservation(envelope.Observation)
	if err != nil {
		t.Fatalf("fixture observation rejected: %v", err)
	}
	if err := ValidateLifecycleObservation(observation, call, clock); err != nil {
		t.Fatalf("fixture observation invalid: %v", err)
	}
	if observation.State != LifecycleStateRunning || !observation.AdmissionReady {
		t.Fatalf("fixture observation not ready: %+v", observation)
	}
	authorization, err := DecodeLifecycleVerification(envelope.Authorization)
	if err != nil {
		t.Fatalf("fixture authorization rejected: %v", err)
	}
	if authorization.Call != call || !authorization.DesiredEnabled {
		t.Fatalf("fixture authorization wrong: %+v", authorization)
	}
	// Interrupted activation adopts a prior recorded effect generation
	// without restart or reenrollment: effect "6" under request "8".
	progressCall, err := DecodeLifecycleCall(envelope.ActivationInProgress.Call)
	if err != nil {
		t.Fatal(err)
	}
	progressObservation, err := DecodeLifecycleObservation(envelope.ActivationInProgress.Observation)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLifecycleObservation(progressObservation, progressCall, clock); err != nil {
		t.Fatalf("interrupted activation invalid: %v", err)
	}
	effect := ""
	if progressObservation.EffectGeneration != nil {
		effect = *progressObservation.EffectGeneration
	}
	if progressObservation.State != LifecycleStateRunning || effect == progressCall.Generation {
		t.Fatalf("prior effect generation not preserved: %+v", progressObservation)
	}
	// Confirmed disable stops with a null effect and no grant.
	disabledCall, err := DecodeLifecycleCall(envelope.DisabledAfterCleanup.Call)
	if err != nil {
		t.Fatal(err)
	}
	disabledObservation, err := DecodeLifecycleObservation(envelope.DisabledAfterCleanup.Observation)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLifecycleObservation(disabledObservation, disabledCall, clock); err != nil {
		t.Fatalf("confirmed disable invalid: %v", err)
	}
	if disabledObservation.State != LifecycleStateStopped ||
		disabledObservation.EffectGeneration != nil || disabledObservation.AdmissionReady {
		t.Fatalf("confirmed disable wrong: %+v", disabledObservation)
	}
	// Failed-enable cleanup: the still-active command's internal
	// admission authorizes exactly its deactivate tuple while desired
	// stays true. It cannot authorize activate and cannot survive the
	// command's completion.
	cleanupCall, err := DecodeLifecycleCall(envelope.FailedEnableCleanup.Call)
	if err != nil {
		t.Fatal(err)
	}
	cleanupAuthorization, err := DecodeLifecycleVerification(envelope.FailedEnableCleanup.Authorization)
	if err != nil {
		t.Fatal(err)
	}
	if cleanupAuthorization.Call != cleanupCall || !cleanupAuthorization.DesiredEnabled {
		t.Fatalf("cleanup authorization wrong: %+v", cleanupAuthorization)
	}
	if cleanupCall.Operation != LifecycleDeactivate {
		t.Fatalf("cleanup call is not a deactivate: %+v", cleanupCall)
	}
	cleanupObservation, err := DecodeLifecycleObservation(envelope.FailedEnableCleanup.Observation)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateLifecycleObservation(cleanupObservation, cleanupCall, clock); err != nil {
		t.Fatalf("cleanup observation invalid: %v", err)
	}
	fixture := &authorizeFixture{t: t, token: "failed-enable-cleanup", admitted: cleanupCall, desired: true, nowUnix: clock}
	socket := serveAuthorizeFixture(t, fixture)
	t.Setenv(CallbackEnv, socket)
	t.Setenv(ServiceCallbackEnv, "")
	authority := LifecycleAuthority{admission: "failed-enable-cleanup", callbackSocket: socket}
	verified, err := authority.Verify(context.Background(), cleanupCall)
	if err != nil {
		t.Fatalf("active cleanup authority rejected: %v", err)
	}
	if verified.Call != cleanupCall || !verified.DesiredEnabled {
		t.Fatalf("active cleanup verification wrong: %+v", verified)
	}
	activate := cleanupCall
	activate.Operation = LifecycleActivate
	if _, err := authority.Verify(context.Background(), activate); err == nil {
		t.Fatal("failed-enable cleanup authorized activate")
	}
	fixture.revoked.Store(true)
	if _, err := authority.Verify(context.Background(), cleanupCall); err == nil {
		t.Fatal("completed command's cleanup authority still verifies")
	}
}
