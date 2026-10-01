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
)

// backgroundVerifier authenticates a background submission. Production uses
// authmodel.VerifySubmission; tests inject a fake. The secret is verified
// and discarded inside the verifier, never logged or recorded here.
type backgroundVerifier func(context.Context, authmodel.SubmissionRequest) (authmodel.SubmissionDecision, error)

// BackgroundMux serves the background dispatcher on a private callback
// socket in front of the browser callback handler. The service socket
// additionally serves bootstrap; per-instance sockets serve their own
// instance's runtime admissions. Browser admissions are never accepted here
// and background admissions are never accepted by the browser handler.
func BackgroundMux(manager *runtime.Manager, browser http.Handler, instanceID string, service bool) http.Handler {
	return backgroundMux(manager, browser, instanceID, service, authmodel.VerifySubmission)
}

func backgroundMux(manager *runtime.Manager, browser http.Handler, instanceID string, service bool, verify backgroundVerifier) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case extension.BackgroundBootstrapPath:
			if !service {
				http.NotFound(w, r)
				return
			}
			serveBackgroundBootstrap(w, r, manager)
			return
		case extension.BackgroundSubmitPath, extension.BackgroundGetPath, extension.BackgroundCancelPath:
			serveBackgroundOperation(w, r, manager, instanceID, service, verify)
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

func serveBackgroundOperation(w http.ResponseWriter, r *http.Request, manager *runtime.Manager, instanceID string, service bool, verify backgroundVerifier) {
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
		serveBackgroundSubmit(w, r, admission, verify)
	case extension.BackgroundGetPath, extension.BackgroundCancelPath:
		serveBackgroundLookup(w, r, admission, r.URL.Path == extension.BackgroundCancelPath)
	}
}

func serveBackgroundSubmit(w http.ResponseWriter, r *http.Request, admission runtime.BackgroundAdmission, verify backgroundVerifier) {
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
	_, err := verify(ctx, authmodel.SubmissionRequest{
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
	// Admitted. Durable operation semantics arrive with the operation
	// implementations; FT02 reports the missing stage rather than inventing
	// a receipt.
	writeBackgroundJSON(w, extension.OperationRecord{
		InstallationID: admission.InstallationID,
		OperationID:    request.OperationID,
		Outcome:        extension.BackgroundOutcomeOperationsUnavailable,
	})
}

func serveBackgroundLookup(w http.ResponseWriter, r *http.Request, admission runtime.BackgroundAdmission, cancel bool) {
	var request struct {
		OperationID string `json:"operation_id"`
	}
	if !decodeBackgroundRequest(w, r, &request) || !validBackgroundOperationID(request.OperationID) {
		http.Error(w, "invalid background request", http.StatusBadRequest)
		return
	}
	// Owning-installation admission is verified above; the lookup itself
	// needs no still-valid PAT or repository permission. FT02 stores no
	// operations, so lookup is honestly not_observed and cancellation
	// honestly reports the missing stage.
	if !cancel {
		writeBackgroundJSON(w, extension.OperationLookup{
			InstallationID: admission.InstallationID,
			OperationID:    request.OperationID,
			Status:         extension.BackgroundOutcomeNotObserved,
		})
		return
	}
	writeBackgroundJSON(w, extension.OperationRecord{
		InstallationID: admission.InstallationID,
		OperationID:    request.OperationID,
		Outcome:        extension.BackgroundOutcomeOperationsUnavailable,
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
