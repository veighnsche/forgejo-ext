// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	extension "forgejo.org/extension-sdk"
	authmodel "forgejo.org/models/extensionauth"
	runtime "forgejo.org/services/extensions"
	operation_service "forgejo.org/services/nativeoperation"

	"github.com/stretchr/testify/require"
)

func backgroundTestManager(t *testing.T) *runtime.Manager {
	t.Helper()
	return runtime.NewManager(t.TempDir())
}

func backgroundPeerAddr(t *testing.T) string {
	t.Helper()
	return extension.FormatUnixPeer(extension.UnixPeer{UID: uint32(os.Geteuid()), GID: 1, PID: 1234})
}

func issueRuntimeAdmission(t *testing.T, manager *runtime.Manager, installation, instance string, capabilities []string) string {
	t.Helper()
	token, err := manager.IssueRuntimeAdmission(installation, instance, capabilities)
	require.NoError(t, err)
	return token
}

func backgroundRequest(t *testing.T, handler http.Handler, path, token, remoteAddr string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set(extension.AdmissionHeader, token)
	}
	request.RemoteAddr = remoteAddr
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func acceptVerifier(context.Context, authmodel.SubmissionRequest) (authmodel.SubmissionDecision, error) {
	return authmodel.SubmissionDecision{TokenID: 9, ActorID: 2, CredentialFingerprint: "fingerprint"}, nil
}

type stubOperations struct {
	revision operation_service.NativeRevisionObservation
	submit   func(context.Context, authmodel.SubmissionDecision, string, *operation_service.ValidIntent) (extension.OperationRecord, error)
	get      func(context.Context, string, string) (extension.OperationLookup, error)
	cancel   func(context.Context, string, string) (extension.OperationRecord, error)
}

func (s stubOperations) ReadNativeRevision(context.Context) (operation_service.NativeRevisionObservation, error) {
	return s.revision, nil
}

func (s stubOperations) Submit(ctx context.Context, decision authmodel.SubmissionDecision, installation string, intent *operation_service.ValidIntent) (extension.OperationRecord, error) {
	return s.submit(ctx, decision, installation, intent)
}

func (s stubOperations) Get(ctx context.Context, installation, operation string) (extension.OperationLookup, error) {
	return s.get(ctx, installation, operation)
}

func (s stubOperations) Cancel(ctx context.Context, installation, operation string) (extension.OperationRecord, error) {
	return s.cancel(ctx, installation, operation)
}

func mergeSubmitPayload() map[string]any {
	return map[string]any{
		"operation_id": "op-1", "actor_id": "2", "repository_id": "1",
		"kind": "pull_request.merge", "authorization_revision": "rev-1",
		"expected_native_revision": 7, "not_after": time.Now().Unix() + 300,
		"payload": map[string]any{
			"pull_request_number": 3, "head_repository_id": 1,
			"head_ref": "refs/heads/branch2", "base_ref": "refs/heads/master",
			"expected_head_oid": "1111111111111111111111111111111111111111",
			"expected_base_oid": "2222222222222222222222222222222222222222",
			"method":            "fast-forward-only",
		},
		"token": "secret",
	}
}

func testOperations() stubOperations {
	return stubOperations{
		revision: operation_service.NativeRevisionObservation{Revision: 7, Idle: true},
		submit: func(_ context.Context, _ authmodel.SubmissionDecision, installation string, intent *operation_service.ValidIntent) (extension.OperationRecord, error) {
			return extension.OperationRecord{
				InstallationID: installation, OperationID: intent.OperationID,
				Outcome: extension.EffectCommitted, EffectState: extension.EffectCommitted,
			}, nil
		},
		get: func(_ context.Context, installation, operation string) (extension.OperationLookup, error) {
			return extension.OperationLookup{InstallationID: installation, OperationID: operation, Status: extension.BackgroundOutcomeNotObserved}, nil
		},
		cancel: func(_ context.Context, installation, operation string) (extension.OperationRecord, error) {
			return extension.OperationRecord{
				InstallationID: installation, OperationID: operation,
				Outcome: extension.EffectNotCommitted, EffectState: extension.EffectNotCommitted,
				CancellationStatus: extension.CancellationCancelled,
			}, nil
		},
	}
}

func TestBackgroundSubmitAdmitsBoundCaller(t *testing.T) {
	manager := backgroundTestManager(t)
	const installation, instance = "11111111-2222-4333-8444-555555555555", "instance-a"
	token := issueRuntimeAdmission(t, manager, installation, instance, []string{extension.CapabilityBackgroundOperations})
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	handler := backgroundMux(manager, browser, instance, false, acceptVerifier, testOperations())

	recorder := backgroundRequest(t, handler, extension.BackgroundSubmitPath, token, backgroundPeerAddr(t), mergeSubmitPayload())
	require.Equal(t, http.StatusOK, recorder.Code)
	var record extension.OperationRecord
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &record))
	require.Equal(t, installation, record.InstallationID)
	require.Equal(t, "op-1", record.OperationID)
	require.Equal(t, extension.EffectCommitted, record.Outcome)

	// A caller installation that conflicts with the admission is forged.
	forged := mergeSubmitPayload()
	forged["installation_id"] = "22222222-2222-4333-8444-555555555555"
	recorder = backgroundRequest(t, handler, extension.BackgroundSubmitPath, token, backgroundPeerAddr(t), forged)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	// An expired admission deadline fails validation, not verification.
	expired := mergeSubmitPayload()
	expired["not_after"] = time.Now().Unix() - 1
	recorder = backgroundRequest(t, handler, extension.BackgroundSubmitPath, token, backgroundPeerAddr(t), expired)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	// Non-background paths still reach the browser handler.
	recorder = backgroundRequest(t, handler, extension.NativeCallbackPath, token, backgroundPeerAddr(t), map[string]any{})
	require.Equal(t, http.StatusTeapot, recorder.Code)
}

func TestBackgroundSubmitMapsServiceOutcomes(t *testing.T) {
	manager := backgroundTestManager(t)
	const installation, instance = "11111111-2222-4333-8444-555555555555", "instance-a"
	token := issueRuntimeAdmission(t, manager, installation, instance, []string{extension.CapabilityBackgroundOperations})
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	cases := []struct {
		name   string
		err    error
		status int
		extra  string
	}{
		{"unimplemented kind reports its missing stage", operation_service.ErrKindUnavailable, http.StatusOK, extension.BackgroundOutcomeOperationsUnavailable},
		{"busy reports unavailable", operation_service.ErrBusy, http.StatusServiceUnavailable, ""},
		{"changed intent conflicts", operation_service.ErrIntentConflict, http.StatusConflict, ""},
		{"cancelled ID conflicts", operation_service.ErrCancelledBeforeSubmit, http.StatusConflict, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			operations := testOperations()
			operations.submit = func(context.Context, authmodel.SubmissionDecision, string, *operation_service.ValidIntent) (extension.OperationRecord, error) {
				return extension.OperationRecord{}, tc.err
			}
			handler := backgroundMux(manager, browser, instance, false, acceptVerifier, operations)
			recorder := backgroundRequest(t, handler, extension.BackgroundSubmitPath, token, backgroundPeerAddr(t), mergeSubmitPayload())
			require.Equal(t, tc.status, recorder.Code)
			if tc.extra != "" {
				require.Contains(t, recorder.Body.String(), tc.extra)
			}
		})
	}
}

func TestBackgroundAdmissionBoundaries(t *testing.T) {
	manager := backgroundTestManager(t)
	const installation = "11111111-2222-4333-8444-555555555555"
	runtimeToken := issueRuntimeAdmission(t, manager, installation, "instance-a", []string{extension.CapabilityBackgroundOperations})
	uncappedToken := issueRuntimeAdmission(t, manager, installation, "instance-a", []string{extension.CapabilityActorRead})
	otherInstanceToken := issueRuntimeAdmission(t, manager, installation, "instance-b", []string{extension.CapabilityBackgroundOperations})
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	handler := backgroundMux(manager, browser, "instance-a", false, acceptVerifier, testOperations())
	peer := backgroundPeerAddr(t)
	submit := map[string]any{
		"operation_id": "op-1", "actor_id": "2", "repository_id": "1",
		"kind": "pull_request.merge", "token": "secret",
	}

	// Unknown admissions, including live browser admissions from the other
	// registry, are never accepted here.
	recorder := backgroundRequest(t, handler, extension.BackgroundSubmitPath, "0123456789abcdefghij0123456789abcdefghijk", peer, submit)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	recorder = backgroundRequest(t, handler, extension.BackgroundGetPath, "", peer, map[string]any{"operation_id": "op-1"})
	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	// Admissions from another instance, and admissions without the
	// background capability, are rejected.
	recorder = backgroundRequest(t, handler, extension.BackgroundSubmitPath, otherInstanceToken, peer, submit)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	recorder = backgroundRequest(t, handler, extension.BackgroundSubmitPath, uncappedToken, peer, submit)
	require.Equal(t, http.StatusForbidden, recorder.Code)

	// A peer that is not the host process is rejected on the runtime socket.
	foreignPeer := extension.FormatUnixPeer(extension.UnixPeer{UID: 42424242, GID: 1, PID: 9999})
	recorder = backgroundRequest(t, handler, extension.BackgroundSubmitPath, runtimeToken, foreignPeer, submit)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	recorder = backgroundRequest(t, handler, extension.BackgroundSubmitPath, runtimeToken, "not-a-peer", submit)
	require.Equal(t, http.StatusForbidden, recorder.Code)

	// Stale admissions from a stopped runtime are rejected.
	manager.RevokeBackgroundForInstance("instance-a")
	recorder = backgroundRequest(t, handler, extension.BackgroundGetPath, runtimeToken, peer, map[string]any{"operation_id": "op-1"})
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestBackgroundLookupCancelAndRevision(t *testing.T) {
	manager := backgroundTestManager(t)
	const installation = "11111111-2222-4333-8444-555555555555"
	token := issueRuntimeAdmission(t, manager, installation, "instance-a", []string{extension.CapabilityBackgroundOperations})
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	handler := backgroundMux(manager, browser, "instance-a", false, acceptVerifier, testOperations())
	peer := backgroundPeerAddr(t)

	recorder := backgroundRequest(t, handler, extension.BackgroundGetPath, token, peer, map[string]any{"operation_id": "op-9"})
	require.Equal(t, http.StatusOK, recorder.Code)
	var lookup extension.OperationLookup
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &lookup))
	require.Equal(t, extension.BackgroundOutcomeNotObserved, lookup.Status)

	recorder = backgroundRequest(t, handler, extension.BackgroundCancelPath, token, peer, map[string]any{"operation_id": "op-9"})
	require.Equal(t, http.StatusOK, recorder.Code)
	var record extension.OperationRecord
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &record))
	require.Equal(t, extension.EffectNotCommitted, record.Outcome)
	require.Equal(t, extension.CancellationCancelled, record.CancellationStatus)

	recorder = backgroundRequest(t, handler, extension.BackgroundRevisionPath, token, peer, map[string]any{})
	require.Equal(t, http.StatusOK, recorder.Code)
	var observation extension.NativeRevisionObservation
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &observation))
	require.Equal(t, int64(7), observation.Revision)
	require.True(t, observation.Idle)
}

func TestBackgroundVerifierRefusalsMapToStatus(t *testing.T) {
	manager := backgroundTestManager(t)
	token := issueRuntimeAdmission(t, manager, "11111111-2222-4333-8444-555555555555", "instance-a", []string{extension.CapabilityBackgroundOperations})
	verify := func(context.Context, authmodel.SubmissionRequest) (authmodel.SubmissionDecision, error) {
		return authmodel.SubmissionDecision{}, &authmodel.RefusalError{Code: authmodel.RefusalBindingMissing, Status: http.StatusForbidden}
	}
	handler := backgroundMux(manager, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), "instance-a", false, verify, testOperations())
	recorder := backgroundRequest(t, handler, extension.BackgroundSubmitPath, token, backgroundPeerAddr(t), map[string]any{
		"operation_id": "op-1", "actor_id": "2", "repository_id": "1",
		"kind": "pull_request.merge", "token": "secret",
	})
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), authmodel.RefusalBindingMissing)
}

func TestBackgroundBootstrapRequiresServiceMapping(t *testing.T) {
	manager := backgroundTestManager(t)
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	// The runtime socket never serves bootstrap.
	runtimeHandler := backgroundMux(manager, browser, "instance-a", false, acceptVerifier, testOperations())
	recorder := backgroundRequest(t, runtimeHandler, extension.BackgroundBootstrapPath, "", backgroundPeerAddr(t), map[string]any{})
	require.Equal(t, http.StatusNotFound, recorder.Code)

	// The service socket rejects unmapped peers without a browser.
	serviceHandler := backgroundMux(manager, browser, "", true, acceptVerifier, testOperations())
	recorder = backgroundRequest(t, serviceHandler, extension.BackgroundBootstrapPath, "", backgroundPeerAddr(t), map[string]any{})
	require.Equal(t, http.StatusForbidden, recorder.Code)
}
