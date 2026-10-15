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
	"time"

	extension "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"
	runtime "forgejo.org/services/extensions"
)

// Manager-control HTTP endpoints (RCX-2 F04, frozen fountain-design
// section 8). These ride the existing shared service callback socket
// behind kernel peer mapping, independent of the optional package's
// runtime or browser admission. They are never public HTTP and never
// accept per-instance browser callback routing: only SERVICE_BRIDGE_PEERS
// UIDs mapped to a required installed lifecycle controller are admitted.
//
// Bounds: state 5s, snapshot 10s, reconcile 300s, cache 600s.
// State/snapshot/reconcile requests are at most 16 KiB; cache at most
// 2 MiB. Every response is at most 128 KiB. Caller cancellation
// propagates; there is no automatic retransmission.
const (
	managerControlBodyLimit = 16 << 10
	managerCacheBodyLimit   = 2 << 20
	managerResponseLimit    = 128 << 10
)

// ManagerControlMux serves the four manager-control endpoints in front
// of the service callback handler. Unknown paths delegate untouched.
func ManagerControlMux(manager *runtime.Manager, next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case runtime.ManagerControlStatePath,
			runtime.ManagerControlSnapshotPath,
			runtime.ManagerControlReconcilePath,
			runtime.ManagerControlCachePath:
			serveManagerControl(w, r, manager)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func serveManagerControl(w http.ResponseWriter, r *http.Request, manager *runtime.Manager) {
	if manager == nil {
		http.Error(w, "extension manager is unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "manager control requires a JSON body", http.StatusBadRequest)
		return
	}
	controllerID, ok := manager.ControlPeer(r.RemoteAddr)
	if !ok {
		http.Error(w, "manager control peer is not authorized", http.StatusForbidden)
		return
	}
	limit := managerControlBodyLimit
	timeout := 5 * time.Second
	switch r.URL.Path {
	case runtime.ManagerControlSnapshotPath:
		timeout = 10 * time.Second
	case runtime.ManagerControlReconcilePath:
		timeout = 300 * time.Second
	case runtime.ManagerControlCachePath:
		limit = managerCacheBodyLimit
		timeout = 600 * time.Second
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)+1))
	if err != nil || len(body) > limit {
		http.Error(w, "manager control request exceeds limit", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	switch r.URL.Path {
	case runtime.ManagerControlStatePath:
		serveControlState(ctx, w, body, manager, controllerID)
	case runtime.ManagerControlSnapshotPath:
		serveControlSnapshot(ctx, w, body, manager, controllerID)
	case runtime.ManagerControlReconcilePath:
		serveControlReconcile(ctx, w, body, manager, controllerID)
	default:
		serveControlCache(ctx, w, body, manager, controllerID)
	}
}

func writeControlJSON(w http.ResponseWriter, value any) {
	data, err := json.Marshal(value)
	if err != nil || len(data) > managerResponseLimit {
		http.Error(w, "manager control response is unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeControlReject(w http.ResponseWriter, reject *runtime.ControlError) {
	if reject == nil {
		http.Error(w, "manager control is unavailable", http.StatusServiceUnavailable)
		return
	}
	http.Error(w, reject.Message, reject.HTTPStatus)
}

// decodeStrictObject decodes one JSON object with no unknown fields and
// no trailing data.
func decodeStrictObject(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}

func serveControlState(ctx context.Context, w http.ResponseWriter, body []byte, manager *runtime.Manager, controllerID string) {
	var request struct {
		PackageID string `json:"package_id"`
	}
	if err := decodeStrictObject(body, &request); err != nil || !extension.ValidID(request.PackageID) {
		http.Error(w, "invalid manager state request", http.StatusBadRequest)
		return
	}
	response, reject := manager.ControlState(ctx, controllerID, request.PackageID)
	if reject != nil {
		writeControlReject(w, reject)
		return
	}
	writeControlJSON(w, response)
}

func serveControlSnapshot(ctx context.Context, w http.ResponseWriter, body []byte, manager *runtime.Manager, controllerID string) {
	var request struct{}
	if err := decodeStrictObject(body, &request); err != nil {
		http.Error(w, "invalid manager snapshot request", http.StatusBadRequest)
		return
	}
	response, reject := manager.ControlSnapshot(ctx, controllerID)
	if reject != nil {
		writeControlReject(w, reject)
		return
	}
	writeControlJSON(w, response)
}

func serveControlReconcile(ctx context.Context, w http.ResponseWriter, body []byte, manager *runtime.Manager, controllerID string) {
	var request struct{}
	if err := decodeStrictObject(body, &request); err != nil {
		http.Error(w, "invalid manager reconcile request", http.StatusBadRequest)
		return
	}
	response, reject := manager.ControlReconcile(ctx, controllerID)
	if reject != nil {
		writeControlReject(w, reject)
		return
	}
	writeControlJSON(w, response)
}

func serveControlCache(ctx context.Context, w http.ResponseWriter, body []byte, manager *runtime.Manager, controllerID string) {
	var request struct {
		Catalog        json.RawMessage `json:"catalog"`
		PackageIDs     []string        `json:"package_ids"`
		SourceSnapshot json.RawMessage `json:"source_snapshot"`
	}
	if err := decodeStrictObject(body, &request); err != nil {
		http.Error(w, "invalid manager cache request", http.StatusBadRequest)
		return
	}
	catalog, err := extension.DecodeCatalog(request.Catalog)
	if err != nil {
		http.Error(w, "invalid manager cache catalog", http.StatusBadRequest)
		return
	}
	source, err := packages.DecodeSnapshot(request.SourceSnapshot)
	if err != nil {
		http.Error(w, "invalid manager cache snapshot", http.StatusBadRequest)
		return
	}
	for _, id := range request.PackageIDs {
		if !extension.ValidID(id) {
			http.Error(w, "invalid manager cache selection", http.StatusBadRequest)
			return
		}
	}
	response, reject := manager.ControlCache(ctx, controllerID, catalog, request.PackageIDs, source)
	if reject != nil {
		writeControlReject(w, reject)
		return
	}
	writeControlJSON(w, response)
}
