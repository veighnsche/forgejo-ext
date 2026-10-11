// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package extensions

import (
	"net/http"

	runtime "forgejo.org/services/extensions"
)

// serveBackgroundOperations answers the read-only operations handshake
// for one admitted installation. Admission, instance binding, peer
// identity and the background capability were already verified by the
// shared dispatcher; installation here is host-derived from that
// admission, never caller-supplied.
func serveBackgroundOperations(w http.ResponseWriter, r *http.Request, admission runtime.BackgroundAdmission, operations backgroundOperations) {
	var request struct{}
	if !decodeBackgroundRequest(w, r, &request) {
		return
	}
	handshake, err := operations.SupportedOperations(r.Context())
	if err != nil {
		http.Error(w, "background operations unavailable", http.StatusServiceUnavailable)
		return
	}
	writeBackgroundJSON(w, map[string]any{
		"installation": admission.InstallationID,
		"generation":   handshake.Generation,
		"kinds":        handshake.Kinds,
	})
}
