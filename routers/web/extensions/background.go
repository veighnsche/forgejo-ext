// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	extension "forgejo.org/extension-sdk"
	authmodel "forgejo.org/models/extensionauth"
	runtime "forgejo.org/services/extensions"
	operation_service "forgejo.org/services/nativeoperation"
)

// backgroundVerifier authenticates a background submission. Production uses
// authmodel.VerifySubmission; tests inject a fake. The secret is verified
// and discarded inside the verifier, never logged or recorded here.
type backgroundVerifier func(context.Context, authmodel.SubmissionRequest) (authmodel.SubmissionDecision, error)

// snapshotVerifier authenticates a background snapshot read. Production uses
// authmodel.VerifySnapshotRead; tests inject a fake. Reads require only
// read scope and read access, and grant no mutation kind.
type snapshotVerifier func(context.Context, authmodel.SnapshotReadRequest) (authmodel.SubmissionDecision, error)

// backgroundVerifiers groups the submission and snapshot-read verifiers so
// dispatch tests inject both together.
type backgroundVerifiers struct {
	submit   backgroundVerifier
	snapshot snapshotVerifier
}

// backgroundOperations executes durable submit/lookup/cancel, revision
// observations and permission-checked snapshot reads. Production uses the
// native-operation service; tests inject a fake. Snapshot reads claim no
// reservation and perform no mutation.
type backgroundOperations interface {
	ReadNativeRevision(context.Context) (operation_service.NativeRevisionObservation, error)
	ReadSnapshot(context.Context, int64, extension.SnapshotRequest) (extension.NativeSnapshot, error)
	Submit(context.Context, authmodel.SubmissionDecision, string, *operation_service.ValidIntent) (extension.OperationRecord, error)
	Get(context.Context, string, string) (extension.OperationLookup, error)
	Cancel(context.Context, string, string) (extension.OperationRecord, error)
}

// BackgroundMux serves the background dispatcher on a private callback
// socket in front of the browser callback handler. The service socket
// additionally serves bootstrap; per-instance sockets serve their own
// instance's runtime admissions. Browser admissions are never accepted here
// and background admissions are never accepted by the browser handler.
func BackgroundMux(manager *runtime.Manager, browser http.Handler, instanceID string, service bool) http.Handler {
	return backgroundMux(manager, browser, instanceID, service, backgroundVerifiers{
		submit:   authmodel.VerifySubmission,
		snapshot: authmodel.VerifySnapshotRead,
	}, operation_service.Default())
}

func backgroundMux(manager *runtime.Manager, browser http.Handler, instanceID string, service bool, verifiers backgroundVerifiers, operations backgroundOperations) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case extension.BackgroundBootstrapPath:
			if !service {
				http.NotFound(w, r)
				return
			}
			serveBackgroundBootstrap(w, r, manager)
			return
		case extension.BackgroundSubmitPath, extension.BackgroundGetPath, extension.BackgroundCancelPath, extension.BackgroundRevisionPath, extension.BackgroundSnapshotPath:
			serveBackgroundOperation(w, r, manager, instanceID, service, verifiers, operations)
			return
		default:
			browser.ServeHTTP(w, r)
		}
	})
}

func serveBackgroundBootstrap(w http.ResponseWriter, r *http.Request, manager *runtime.Manager) {
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "invalid background bootstrap request", http.StatusBadRequest)
		return
	}
	peer, err := extension.ParseUnixPeer(r.RemoteAddr)
	if err != nil {
		http.Error(w, "background peer identity unavailable", http.StatusForbidden)
		return
	}
	var request struct {
		InstallationID string `json:"installation_id"`
	}
	if !decodeBackgroundRequest(w, r, &request) {
		return
	}
	token, installation, err := manager.BootstrapServiceAdmission(peer.UID, request.InstallationID)
	if err != nil {
		http.Error(w, "service bridge bootstrap rejected", http.StatusForbidden)
		return
	}
	writeBackgroundJSON(w, struct {
		Admission      string `json:"admission"`
		InstallationID string `json:"installation_id"`
	}{Admission: token, InstallationID: installation})
}

func serveBackgroundOperation(w http.ResponseWriter, r *http.Request, manager *runtime.Manager, instanceID string, service bool, verifiers backgroundVerifiers, operations backgroundOperations) {
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "invalid background request", http.StatusBadRequest)
		return
	}
	token := r.Header.Get(extension.AdmissionHeader)
	if len(token) != 43 {
		http.Error(w, "background admission required", http.StatusUnauthorized)
		return
	}
	admission, ok := manager.VerifyBackgroundAdmission(token)
	if !ok {
		http.Error(w, "background admission expired", http.StatusUnauthorized)
		return
	}
	// Each channel accepts only its own admissions: runtime admissions stay
	// on their per-instance socket and service admissions on the shared one.
	if service != admission.Service {
		http.Error(w, "background admission rejected", http.StatusForbidden)
		return
	}
	if !service && admission.InstanceID != instanceID {
		http.Error(w, "background admission rejected", http.StatusForbidden)
		return
	}
	peer, err := extension.ParseUnixPeer(r.RemoteAddr)
	if err != nil {
		http.Error(w, "background peer identity unavailable", http.StatusForbidden)
		return
	}
	if service {
		if peer.UID != admission.ServicePeerUID {
			http.Error(w, "background peer rejected", http.StatusForbidden)
			return
		}
	} else if peer.UID != runtime.CurrentProcessUID() {
		http.Error(w, "background peer rejected", http.StatusForbidden)
		return
	}
	if _, declared := admission.Capabilities[extension.CapabilityBackgroundOperations]; !declared {
		http.Error(w, "background capability not declared", http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case extension.BackgroundSubmitPath:
		serveBackgroundSubmit(w, r, admission, verifiers.submit, operations)
	case extension.BackgroundGetPath:
		serveBackgroundGet(w, r, admission, operations)
	case extension.BackgroundCancelPath:
		serveBackgroundCancel(w, r, admission, operations)
	case extension.BackgroundRevisionPath:
		serveBackgroundRevision(w, r, operations)
	case extension.BackgroundSnapshotPath:
		serveBackgroundSnapshot(w, r, admission, verifiers.snapshot, operations)
	}
}

func serveBackgroundSubmit(w http.ResponseWriter, r *http.Request, admission runtime.BackgroundAdmission, verify backgroundVerifier, operations backgroundOperations) {
	var request struct {
		extension.OperationIntent
		Token string `json:"token"`
	}
	if !decodeBackgroundRequest(w, r, &request) {
		return
	}
	if !validBackgroundOperationID(request.OperationID) ||
		request.InstallationID != "" && request.InstallationID != admission.InstallationID ||
		mustParsePositiveID(request.ActorID) == 0 || mustParsePositiveID(request.RepositoryID) == 0 ||
		request.Kind == "" || request.Token == "" {
		http.Error(w, "invalid background intent", http.StatusBadRequest)
		return
	}
	actorID, _ := strconv.ParseInt(request.ActorID, 10, 64)
	repositoryID, _ := strconv.ParseInt(request.RepositoryID, 10, 64)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	decision, err := verify(ctx, authmodel.SubmissionRequest{
		InstallationID: admission.InstallationID,
		TokenSecret:    request.Token,
		ActorID:        actorID,
		RepositoryID:   repositoryID,
		Kind:           request.Kind,
	})
	if err != nil {
		var refusal *authmodel.RefusalError
		if errors.As(err, &refusal) {
			http.Error(w, refusal.Code, refusal.Status)
			return
		}
		http.Error(w, "background submission unavailable", http.StatusServiceUnavailable)
		return
	}
	intent, err := operation_service.ValidateIntent(request.OperationID, actorID, repositoryID, request.Kind,
		request.AuthorizationRevision, request.ExpectedNativeRevision, request.NotAfter, request.Payload, time.Now().Unix())
	if err != nil {
		http.Error(w, "invalid background intent", http.StatusBadRequest)
		return
	}
	// Submission executes the guarded native write synchronously, so it
	// runs past the short verification timeout above.
	record, err := operations.Submit(r.Context(), decision, admission.InstallationID, intent)
	if err != nil {
		switch {
		case errors.Is(err, operation_service.ErrKindUnavailable):
			writeBackgroundJSON(w, extension.OperationRecord{
				InstallationID: admission.InstallationID,
				OperationID:    request.OperationID,
				Outcome:        extension.BackgroundOutcomeOperationsUnavailable,
			})
		case operation_service.IsBusy(err):
			http.Error(w, "native operation in progress", http.StatusServiceUnavailable)
		case errors.Is(err, operation_service.ErrIntentConflict):
			http.Error(w, "intent_conflict", http.StatusConflict)
		case errors.Is(err, operation_service.ErrCancelledBeforeSubmit):
			http.Error(w, "cancelled_before_submit", http.StatusConflict)
		case errors.Is(err, operation_service.ErrInvalidIntent):
			http.Error(w, "invalid background intent", http.StatusBadRequest)
		default:
			http.Error(w, "background submission unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	writeBackgroundJSON(w, record)
}

func serveBackgroundGet(w http.ResponseWriter, r *http.Request, admission runtime.BackgroundAdmission, operations backgroundOperations) {
	var request struct {
		OperationID string `json:"operation_id"`
	}
	if !decodeBackgroundRequest(w, r, &request) || !validBackgroundOperationID(request.OperationID) {
		http.Error(w, "invalid background request", http.StatusBadRequest)
		return
	}
	// Owning-installation admission is verified above; the lookup itself
	// needs no still-valid PAT or repository permission.
	lookup, err := operations.Get(r.Context(), admission.InstallationID, request.OperationID)
	if err != nil {
		http.Error(w, "background lookup unavailable", http.StatusServiceUnavailable)
		return
	}
	writeBackgroundJSON(w, lookup)
}

func serveBackgroundCancel(w http.ResponseWriter, r *http.Request, admission runtime.BackgroundAdmission, operations backgroundOperations) {
	var request struct {
		OperationID string `json:"operation_id"`
	}
	if !decodeBackgroundRequest(w, r, &request) || !validBackgroundOperationID(request.OperationID) {
		http.Error(w, "invalid background request", http.StatusBadRequest)
		return
	}
	// Owned cancellation stays available after actor-token or binding
	// withdrawal; it needs no still-valid PAT or repository permission.
	record, err := operations.Cancel(r.Context(), admission.InstallationID, request.OperationID)
	if err != nil {
		http.Error(w, "background cancellation unavailable", http.StatusServiceUnavailable)
		return
	}
	writeBackgroundJSON(w, record)
}

// serveBackgroundSnapshot serves one permission-checked native snapshot
// read. Admission and read verification mirror submission; the read itself
// claims no reservation and performs no mutation. Missing and gated records
// share one bounded not-found outcome so responses leak no existence
// distinction.
func serveBackgroundSnapshot(w http.ResponseWriter, r *http.Request, admission runtime.BackgroundAdmission, verify snapshotVerifier, operations backgroundOperations) {
	var request struct {
		extension.SnapshotRequest
		Token string `json:"token"`
	}
	if !decodeBackgroundRequest(w, r, &request) {
		return
	}
	if err := extension.ValidateSnapshotRequest(request.SnapshotRequest); err != nil || request.Token == "" {
		http.Error(w, "invalid background snapshot request", http.StatusBadRequest)
		return
	}
	actorID, _ := strconv.ParseInt(request.ActorID, 10, 64)
	repositoryID, _ := strconv.ParseInt(request.RepositoryID, 10, 64)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	decision, err := verify(ctx, authmodel.SnapshotReadRequest{
		InstallationID: admission.InstallationID,
		TokenSecret:    request.Token,
		ActorID:        actorID,
		RepositoryID:   repositoryID,
	})
	if err != nil {
		var refusal *authmodel.RefusalError
		if errors.As(err, &refusal) {
			http.Error(w, refusal.Code, refusal.Status)
			return
		}
		http.Error(w, "background snapshot unavailable", http.StatusServiceUnavailable)
		return
	}
	snapshot, err := operations.ReadSnapshot(r.Context(), decision.ActorID, request.SnapshotRequest)
	if err != nil {
		switch {
		case errors.Is(err, operation_service.ErrSnapshotInvalid):
			http.Error(w, "invalid background snapshot request", http.StatusBadRequest)
		case errors.Is(err, operation_service.ErrSnapshotNotFound):
			http.Error(w, "snapshot_not_found", http.StatusNotFound)
		default:
			http.Error(w, "background snapshot unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	writeBackgroundJSON(w, snapshot)
}

func serveBackgroundRevision(w http.ResponseWriter, r *http.Request, operations backgroundOperations) {
	var request struct{}
	if !decodeBackgroundRequest(w, r, &request) {
		return
	}
	observation, err := operations.ReadNativeRevision(r.Context())
	if err != nil {
		http.Error(w, "background revision unavailable", http.StatusServiceUnavailable)
		return
	}
	writeBackgroundJSON(w, extension.NativeRevisionObservation{
		Revision: observation.Revision,
		Idle:     observation.Idle,
	})
}

func validBackgroundOperationID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return true
}

func decodeBackgroundRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.ContentLength > maxCallbackRequestBytes {
		http.Error(w, "background request exceeds limits", http.StatusRequestEntityTooLarge)
		return false
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackRequestBytes+1))
	if err != nil || len(data) > maxCallbackRequestBytes {
		http.Error(w, "invalid background request", http.StatusBadRequest)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(w, "invalid background request", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid background request", http.StatusBadRequest)
		return false
	}
	return true
}

func writeBackgroundJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}
