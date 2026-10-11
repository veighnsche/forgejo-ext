// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package extensions

import (
	"encoding/json"
	"net/http"
	"testing"

	extension "forgejo.org/extension-sdk"

	"github.com/stretchr/testify/require"
)

func TestBackgroundOperationsHandshake(t *testing.T) {
	manager := backgroundTestManager(t)
	const installation = "11111111-2222-4333-8444-555555555555"
	token := issueRuntimeAdmission(t, manager, installation, "instance-a", []string{extension.CapabilityBackgroundOperations})
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	handler := backgroundMux(manager, browser, "instance-a", false, testVerifiers(), testOperations())
	peer := backgroundPeerAddr(t)

	recorder := backgroundRequest(t, handler, extension.BackgroundOperationsPath, token, peer, map[string]any{})
	require.Equal(t, http.StatusOK, recorder.Code)
	var handshake extension.OperationsHandshake
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &handshake))
	require.Equal(t, installation, handshake.Installation)
	require.Equal(t, "ops-test", handshake.Generation)
	require.Equal(t, []string{"issue.lifecycle.apply"}, handshake.Kinds)
}

func TestBackgroundOperationsRequiresCapability(t *testing.T) {
	manager := backgroundTestManager(t)
	const installation = "11111111-2222-4333-8444-555555555555"
	token := issueRuntimeAdmission(t, manager, installation, "instance-a", []string{"other.capability"})
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	handler := backgroundMux(manager, browser, "instance-a", false, testVerifiers(), testOperations())
	peer := backgroundPeerAddr(t)

	recorder := backgroundRequest(t, handler, extension.BackgroundOperationsPath, token, peer, map[string]any{})
	require.Equal(t, http.StatusForbidden, recorder.Code)
}
