// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"context"
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	extension "forgejo.org/extension-sdk"
	extensionauth_model "forgejo.org/models/extensionauth"
	extensions_service "forgejo.org/services/extensions"
	nativeoperation_service "forgejo.org/services/nativeoperation"

	"github.com/stretchr/testify/require"
)

func backgroundTestManager(t *testing.T) *extensions_service.Manager {
	t.Helper()
	return extensions_service.NewManager(t.TempDir())
}

func backgroundPeerAddr(t *testing.T) string {
	t.Helper()
	return extension.FormatUnixPeer(extension.UnixPeer{UID: uint32(os.Geteuid()), GID: 1, PID: 1234})
}

func issueRuntimeAdmission(t *testing.T, manager *extensions_service.Manager, installation, instance string, capabilities []string) string {
	t.Helper()
	// These multiplexer tests never start lifecycle tracking, so the
	// bound package carries no backend or intent state.
	token, err := manager.IssueRuntimeAdmission("multiplexer-test", installation, instance, capabilities)
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

func acceptVerifier(context.Context, extensionauth_model.SubmissionRequest) (extensionauth_model.SubmissionDecision, error) {
	return extensionauth_model.SubmissionDecision{TokenID: 9, ActorID: 2, CredentialFingerprint: "fingerprint"}, nil
}

func acceptSnapshotVerifier(context.Context, extensionauth_model.SnapshotReadRequest) (extensionauth_model.SnapshotReadDecision, error) {
	return extensionauth_model.SnapshotReadDecision{SubmissionDecision: extensionauth_model.SubmissionDecision{TokenID: 9, ActorID: 2, CredentialFingerprint: "fingerprint"}, RepositoryID: 1}, nil
}

func testVerifiers() backgroundVerifiers {
	return backgroundVerifiers{submit: acceptVerifier, snapshot: acceptSnapshotVerifier}
}

type stubOperations struct {
	handshake nativeoperation_service.OperationsHandshake
	revision  nativeoperation_service.NativeRevisionObservation
	snapshot  func(context.Context, extensionauth_model.SnapshotReadDecision, extension.SnapshotRequest) (extension.NativeSnapshot, error)
	creations func(context.Context, extensionauth_model.SnapshotReadDecision, extension.CreationRecoveryRequest) (extension.CreationRecoveryPage, error)
	submit    func(context.Context, extensionauth_model.SubmissionDecision, string, *nativeoperation_service.ValidIntent) (extension.OperationRecord, error)
	get       func(context.Context, string, string) (extension.OperationLookup, error)
	cancel    func(context.Context, string, string) (extension.OperationRecord, error)
}

func (s stubOperations) SupportedOperations(context.Context) (nativeoperation_service.OperationsHandshake, error) {
	return s.handshake, nil
}

func (s stubOperations) ReadNativeRevision(context.Context) (nativeoperation_service.NativeRevisionObservation, error) {
	return s.revision, nil
}

func (s stubOperations) ReadSnapshot(ctx context.Context, decision extensionauth_model.SnapshotReadDecision, req extension.SnapshotRequest) (extension.NativeSnapshot, error) {
	return s.snapshot(ctx, decision, req)
}

func (s stubOperations) ReadCreations(ctx context.Context, decision extensionauth_model.SnapshotReadDecision, req extension.CreationRecoveryRequest) (extension.CreationRecoveryPage, error) {
	return s.creations(ctx, decision, req)
}

func (s stubOperations) Submit(ctx context.Context, decision extensionauth_model.SubmissionDecision, installation string, intent *nativeoperation_service.ValidIntent) (extension.OperationRecord, error) {
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
		handshake: nativeoperation_service.OperationsHandshake{Generation: "ops-test", Kinds: []string{"issue.lifecycle.apply"}},
		revision:  nativeoperation_service.NativeRevisionObservation{Revision: 7, Idle: true},
		snapshot: func(_ context.Context, _ extensionauth_model.SnapshotReadDecision, req extension.SnapshotRequest) (extension.NativeSnapshot, error) {
			return extension.NativeSnapshot{RepositoryID: req.RepositoryID}, nil
		},
		submit: func(_ context.Context, _ extensionauth_model.SubmissionDecision, installation string, intent *nativeoperation_service.ValidIntent) (extension.OperationRecord, error) {
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
	handler := backgroundMux(manager, browser, instance, false, testVerifiers(), testOperations())

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
		{"unimplemented kind reports its missing stage", nativeoperation_service.ErrKindUnavailable, http.StatusOK, extension.BackgroundOutcomeOperationsUnavailable},
		{"busy reports unavailable", nativeoperation_service.ErrBusy, http.StatusServiceUnavailable, ""},
		{"changed intent conflicts", nativeoperation_service.ErrIntentConflict, http.StatusConflict, ""},
		{"cancelled ID conflicts", nativeoperation_service.ErrCancelledBeforeSubmit, http.StatusConflict, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			operations := testOperations()
			operations.submit = func(context.Context, extensionauth_model.SubmissionDecision, string, *nativeoperation_service.ValidIntent) (extension.OperationRecord, error) {
				return extension.OperationRecord{}, tc.err
			}
			handler := backgroundMux(manager, browser, instance, false, testVerifiers(), operations)
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
	handler := backgroundMux(manager, browser, "instance-a", false, testVerifiers(), testOperations())
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
	handler := backgroundMux(manager, browser, "instance-a", false, testVerifiers(), testOperations())
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
	verify := func(context.Context, extensionauth_model.SubmissionRequest) (extensionauth_model.SubmissionDecision, error) {
		return extensionauth_model.SubmissionDecision{}, &extensionauth_model.RefusalError{Code: extensionauth_model.RefusalBindingMissing, Status: http.StatusForbidden}
	}
	handler := backgroundMux(manager, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), "instance-a", false, backgroundVerifiers{submit: verify, snapshot: acceptSnapshotVerifier}, testOperations())
	recorder := backgroundRequest(t, handler, extension.BackgroundSubmitPath, token, backgroundPeerAddr(t), map[string]any{
		"operation_id": "op-1", "actor_id": "2", "repository_id": "1",
		"kind": "pull_request.merge", "token": "secret",
	})
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), extensionauth_model.RefusalBindingMissing)
}

func TestBackgroundBootstrapRequiresServiceMapping(t *testing.T) {
	manager := backgroundTestManager(t)
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	// The runtime socket never serves bootstrap.
	runtimeHandler := backgroundMux(manager, browser, "instance-a", false, testVerifiers(), testOperations())
	recorder := backgroundRequest(t, runtimeHandler, extension.BackgroundBootstrapPath, "", backgroundPeerAddr(t), map[string]any{})
	require.Equal(t, http.StatusNotFound, recorder.Code)

	// The service socket rejects unmapped peers without a browser.
	serviceHandler := backgroundMux(manager, browser, "", true, testVerifiers(), testOperations())
	recorder = backgroundRequest(t, serviceHandler, extension.BackgroundBootstrapPath, "", backgroundPeerAddr(t), map[string]any{})
	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func snapshotReadPayload() map[string]any {
	return map[string]any{
		"repository_id": "1", "actor_id": "2",
		"families":    []string{"issue"},
		"issue_index": "1",
		"token":       "secret",
	}
}

func TestBackgroundSnapshotReadsThroughVerifiedActor(t *testing.T) {
	manager := backgroundTestManager(t)
	const installation, instance = "11111111-2222-4333-8444-555555555555", "instance-a"
	token := issueRuntimeAdmission(t, manager, installation, instance, []string{extension.CapabilityBackgroundOperations})
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	handler := backgroundMux(manager, browser, instance, false, testVerifiers(), testOperations())
	peer := backgroundPeerAddr(t)

	recorder := backgroundRequest(t, handler, extension.BackgroundSnapshotPath, token, peer, snapshotReadPayload())
	require.Equal(t, http.StatusOK, recorder.Code)
	var snapshot extension.NativeSnapshot
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &snapshot))
	require.Equal(t, "1", snapshot.RepositoryID)

	// Unknown admissions are never accepted on the snapshot path either.
	recorder = backgroundRequest(t, handler, extension.BackgroundSnapshotPath, "", peer, snapshotReadPayload())
	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	// Malformed selectors fail before verification.
	malformed := snapshotReadPayload()
	malformed["families"] = []string{"approval"}
	recorder = backgroundRequest(t, handler, extension.BackgroundSnapshotPath, token, peer, malformed)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	missingSecret := snapshotReadPayload()
	delete(missingSecret, "token")
	recorder = backgroundRequest(t, handler, extension.BackgroundSnapshotPath, token, peer, missingSecret)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestBackgroundSnapshotMapsReadOutcomes(t *testing.T) {
	manager := backgroundTestManager(t)
	token := issueRuntimeAdmission(t, manager, "11111111-2222-4333-8444-555555555555", "instance-a", []string{extension.CapabilityBackgroundOperations})
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	peer := backgroundPeerAddr(t)

	// Verifier refusals keep their bounded code and status.
	refusing := backgroundVerifiers{
		submit: acceptVerifier,
		snapshot: func(context.Context, extensionauth_model.SnapshotReadRequest) (extensionauth_model.SnapshotReadDecision, error) {
			return extensionauth_model.SnapshotReadDecision{}, &extensionauth_model.RefusalError{Code: extensionauth_model.RefusalPermissionLost, Status: http.StatusForbidden}
		},
	}
	handler := backgroundMux(manager, browser, "instance-a", false, refusing, testOperations())
	recorder := backgroundRequest(t, handler, extension.BackgroundSnapshotPath, token, peer, snapshotReadPayload())
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), extensionauth_model.RefusalPermissionLost)

	// Missing records share one bounded not-found outcome; service
	// failures are unavailable without leaking record state.
	cases := []struct {
		name   string
		err    error
		status int
		extra  string
	}{
		{"missing record is not found", nativeoperation_service.ErrSnapshotNotFound, http.StatusNotFound, "snapshot_not_found"},
		{"invalid read is rejected", nativeoperation_service.ErrSnapshotInvalid, http.StatusBadRequest, ""},
		{"unavailable read stays unavailable", nativeoperation_service.ErrSnapshotUnavailable, http.StatusServiceUnavailable, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			operations := testOperations()
			operations.snapshot = func(context.Context, extensionauth_model.SnapshotReadDecision, extension.SnapshotRequest) (extension.NativeSnapshot, error) {
				return extension.NativeSnapshot{}, tc.err
			}
			handler := backgroundMux(manager, browser, "instance-a", false, testVerifiers(), operations)
			recorder := backgroundRequest(t, handler, extension.BackgroundSnapshotPath, token, peer, snapshotReadPayload())
			require.Equal(t, tc.status, recorder.Code)
			if tc.extra != "" {
				require.Contains(t, recorder.Body.String(), tc.extra)
			}
		})
	}
}

func TestBackgroundSnapshotPreservesVerifiedCredentialRestrictions(t *testing.T) {
	manager := backgroundTestManager(t)
	const installation = "11111111-2222-4333-8444-555555555555"
	token := issueRuntimeAdmission(t, manager, installation, "instance-a", []string{extension.CapabilityBackgroundOperations})
	var verified extensionauth_model.SnapshotReadDecision
	verifiers := testVerifiers()
	verifiers.snapshot = func(_ context.Context, req extensionauth_model.SnapshotReadRequest) (extensionauth_model.SnapshotReadDecision, error) {
		require.Equal(t, []string{extension.SnapshotFamilyIssue}, req.Families)
		verified = extensionauth_model.SnapshotReadDecision{
			SubmissionDecision: extensionauth_model.SubmissionDecision{ActorID: req.ActorID, TokenID: 9},
			RepositoryID:       req.RepositoryID, Scope: "public-only,read:issue", PublicOnly: true,
			RepositoryIDs: []int64{1},
		}
		return verified, nil
	}
	operations := testOperations()
	operations.snapshot = func(_ context.Context, decision extensionauth_model.SnapshotReadDecision, req extension.SnapshotRequest) (extension.NativeSnapshot, error) {
		require.Equal(t, verified, decision)
		return extension.NativeSnapshot{RepositoryID: req.RepositoryID}, nil
	}
	handler := backgroundMux(manager, http.NotFoundHandler(), "instance-a", false, verifiers, operations)
	recorder := backgroundRequest(t, handler, extension.BackgroundSnapshotPath, token, backgroundPeerAddr(t), snapshotReadPayload())
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestBackgroundCreationsUsesIssueCredentialAndPreservesDecision(t *testing.T) {
	manager := backgroundTestManager(t)
	const installation = "11111111-2222-4333-8444-555555555555"
	token := issueRuntimeAdmission(t, manager, installation, "instance-a", []string{extension.CapabilityBackgroundOperations})
	decision := extensionauth_model.SnapshotReadDecision{SubmissionDecision: extensionauth_model.SubmissionDecision{ActorID: 2, TokenID: 9}, RepositoryID: 1, Scope: "public-only,read:issue", PublicOnly: true, RepositoryIDs: []int64{1}}
	verifiers := testVerifiers()
	verifiers.snapshot = func(_ context.Context, req extensionauth_model.SnapshotReadRequest) (extensionauth_model.SnapshotReadDecision, error) {
		require.Equal(t, installation, req.InstallationID)
		require.Equal(t, []string{extension.SnapshotFamilyIssue}, req.Families)
		return decision, nil
	}
	operations := testOperations()
	operations.creations = func(ctx context.Context, got extensionauth_model.SnapshotReadDecision, req extension.CreationRecoveryRequest) (extension.CreationRecoveryPage, error) {
		require.Equal(t, decision, got)
		_, bounded := ctx.Deadline()
		require.True(t, bounded)
		return extension.CreationRecoveryPage{RepositoryID: req.RepositoryID, Complete: true, ProvenanceComplete: true, RetentionLimited: true}, nil
	}
	handler := backgroundMux(manager, http.NotFoundHandler(), "instance-a", false, verifiers, operations)
	payload := map[string]any{"repository_id": "1", "actor_id": "2", "limit": 10, "token": "native-secret"}
	recorder := backgroundRequest(t, handler, extension.BackgroundCreationsPath, token, backgroundPeerAddr(t), payload)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "native-secret")
	recorder = backgroundRequest(t, handler, extension.BackgroundCreationsPath, "", backgroundPeerAddr(t), payload)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	payload["limit"] = 101
	recorder = backgroundRequest(t, handler, extension.BackgroundCreationsPath, token, backgroundPeerAddr(t), payload)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestBackgroundCreationsMapsNativeRefusalAndUnavailability(t *testing.T) {
	manager := backgroundTestManager(t)
	token := issueRuntimeAdmission(t, manager, "11111111-2222-4333-8444-555555555555", "instance-a", []string{extension.CapabilityBackgroundOperations})
	payload := map[string]any{"repository_id": "1", "actor_id": "2", "limit": 10, "token": "native-secret"}
	for _, tc := range []struct {
		err    error
		status int
	}{
		{nativeoperation_service.ErrSnapshotNotFound, http.StatusNotFound},
		{nativeoperation_service.ErrSnapshotInvalid, http.StatusBadRequest},
		{nativeoperation_service.ErrSnapshotUnavailable, http.StatusServiceUnavailable},
	} {
		operations := testOperations()
		operations.creations = func(context.Context, extensionauth_model.SnapshotReadDecision, extension.CreationRecoveryRequest) (extension.CreationRecoveryPage, error) {
			return extension.CreationRecoveryPage{}, tc.err
		}
		handler := backgroundMux(manager, http.NotFoundHandler(), "instance-a", false, testVerifiers(), operations)
		recorder := backgroundRequest(t, handler, extension.BackgroundCreationsPath, token, backgroundPeerAddr(t), payload)
		require.Equal(t, tc.status, recorder.Code)
	}
	verifiers := testVerifiers()
	verifiers.snapshot = func(context.Context, extensionauth_model.SnapshotReadRequest) (extensionauth_model.SnapshotReadDecision, error) {
		return extensionauth_model.SnapshotReadDecision{}, &extensionauth_model.RefusalError{Code: extensionauth_model.RefusalRepositoryExcluded, Status: http.StatusForbidden}
	}
	handler := backgroundMux(manager, http.NotFoundHandler(), "instance-a", false, verifiers, testOperations())
	recorder := backgroundRequest(t, handler, extension.BackgroundCreationsPath, token, backgroundPeerAddr(t), payload)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), extensionauth_model.RefusalRepositoryExcluded)
}

// TestBackgroundServiceChannelRejectsRuntimeAdmission proves channel
// separation at HTTP dispatch: a runtime admission never dispatches on
// the shared service socket, even for a live peer with the capability
// declared. Unknown admissions still refuse before any channel check.
// A service admission reaching the service mux needs a running package
// behind BootstrapServiceAdmission, so that direction stays covered by
// the manager lifecycle proof rather than this multiplexer case.
func TestBackgroundServiceChannelRejectsRuntimeAdmission(t *testing.T) {
	manager := backgroundTestManager(t)
	const installation = "11111111-2222-4333-8444-555555555555"
	runtimeToken := issueRuntimeAdmission(t, manager, installation, "instance-a", []string{extension.CapabilityBackgroundOperations})
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	serviceHandler := backgroundMux(manager, browser, "", true, testVerifiers(), testOperations())
	peer := backgroundPeerAddr(t)

	recorder := backgroundRequest(t, serviceHandler, extension.BackgroundOperationsPath, runtimeToken, peer, map[string]any{})
	require.Equal(t, http.StatusForbidden, recorder.Code)

	recorder = backgroundRequest(t, serviceHandler, extension.BackgroundOperationsPath, "0123456789abcdefghij0123456789abcdefghijk", peer, map[string]any{})
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
