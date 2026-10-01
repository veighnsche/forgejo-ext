// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"

	"github.com/stretchr/testify/require"
)

func installBackgroundPackage(t *testing.T, root, id string) {
	t.Helper()
	packageDir := filepath.Join(root, id)
	require.NoError(t, os.MkdirAll(packageDir, 0o700))
	executable, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(executable)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension"), data, 0o700))
	manifest := fmt.Sprintf(`{"protocol":1,"id":%q,"name":"Test","version":"1.0","executable":"extension","capabilities":["native.service.bridge","native.background.operations"]}`, id)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(manifest), 0o600))
}

func startBackgroundManager(t *testing.T, root string) (*Manager, uint32) {
	t.Helper()
	installBackgroundPackage(t, root, "bgsample")
	manager := NewManager(root)
	manager.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.NoError(t, manager.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	}))
	require.NoError(t, manager.SetServiceCallbackEndpoint(filepath.Join(root, "service.sock"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	self := uint32(os.Geteuid())
	require.NoError(t, manager.SetServiceBridgePeers(map[uint32]string{self: "bgsample"}))
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(func() { _ = manager.Close() })
	return manager, self
}

func TestServiceBootstrapLifecycle(t *testing.T) {
	root := shortTestRoot(t)
	manager, self := startBackgroundManager(t, root)
	installation, err := packages.LoadInstallation(root, "bgsample")
	require.NoError(t, err)
	require.NotEmpty(t, installation)

	token, resolved, err := manager.BootstrapServiceAdmission(self, "")
	require.NoError(t, err)
	require.Equal(t, installation, resolved)
	claims, ok := manager.VerifyBackgroundAdmission(token)
	require.True(t, ok)
	require.True(t, claims.Service)
	require.Equal(t, installation, claims.InstallationID)
	require.NotEmpty(t, claims.InstanceID)
	require.Contains(t, claims.Capabilities, sdk.CapabilityServiceBridge)
	require.Contains(t, claims.Capabilities, sdk.CapabilityBackgroundOperations)

	// A second bootstrap atomically replaces the delegated admission.
	replacement, _, err := manager.BootstrapServiceAdmission(self, installation)
	require.NoError(t, err)
	require.NotEqual(t, token, replacement)
	_, ok = manager.VerifyBackgroundAdmission(token)
	require.False(t, ok, "replaced service admission must be revoked")

	// Wrong peer, unknown package state and forged installation are rejected.
	_, _, err = manager.BootstrapServiceAdmission(self^0xffffff, "")
	require.ErrorContains(t, err, "not permitted")
	_, _, err = manager.BootstrapServiceAdmission(self, "22222222-2222-4333-8444-555555555555")
	require.ErrorContains(t, err, "installation mismatch")

	// Restart preserves installation identity while replacing admission.
	require.NoError(t, manager.Close())
	_, ok = manager.VerifyBackgroundAdmission(replacement)
	require.False(t, ok, "runtime stop must revoke service admissions")
	require.NoError(t, manager.Start(context.Background()))
	preserved, err := packages.LoadInstallation(root, "bgsample")
	require.NoError(t, err)
	require.Equal(t, installation, preserved)
	fresh, _, err := manager.BootstrapServiceAdmission(self, "")
	require.NoError(t, err)
	require.NotEqual(t, replacement, fresh)
	_, ok = manager.VerifyBackgroundAdmission(replacement)
	require.False(t, ok, "restart must invalidate previous admissions")
}

func TestParseServiceBridgePeers(t *testing.T) {
	mapping, err := ParseServiceBridgePeers("1001:soda,1002:other")
	require.NoError(t, err)
	require.Equal(t, map[uint32]string{1001: "soda", 1002: "other"}, mapping)
	mapping, err = ParseServiceBridgePeers("")
	require.NoError(t, err)
	require.Empty(t, mapping)
	for _, raw := range []string{"soda", "1001:", "1001:Not-An-ID!", "nope:soda", "4294967296:soda", "1001:soda,1001:other"} {
		_, err := ParseServiceBridgePeers(raw)
		require.Error(t, err, raw)
	}
}

func TestBackgroundRegistryRevocation(t *testing.T) {
	registry := newBackgroundRegistry()
	runtime, err := registry.issue(&BackgroundAdmission{InstallationID: "install-1", InstanceID: "a", Capabilities: map[string]struct{}{"cap": {}}})
	require.NoError(t, err)
	service, err := registry.replaceService(&BackgroundAdmission{InstallationID: "install-1", InstanceID: "a", Service: true})
	require.NoError(t, err)
	// Replacement revokes only previous service admissions of the install.
	replacement, err := registry.replaceService(&BackgroundAdmission{InstallationID: "install-1", InstanceID: "a", Service: true})
	require.NoError(t, err)
	_, ok := registry.get(runtime)
	require.True(t, ok)
	_, ok = registry.get(service)
	require.False(t, ok)
	_, ok = registry.get(replacement)
	require.True(t, ok)
	// Stopping the runtime revokes both caller forms.
	registry.revokeInstance("a")
	_, ok = registry.get(runtime)
	require.False(t, ok)
	_, ok = registry.get(replacement)
	require.False(t, ok)
	_, ok = registry.get("0123456789abcdefghij0123456789abcdefghijk")
	require.False(t, ok)
}

// shortUnixSocket returns name under a fresh temp dir, falling back to a
// short CWD-relative path when TMPDIR depth would exceed the unix limit.
func shortUnixSocket(t *testing.T, name string) string {
	t.Helper()
	candidate := filepath.Join(t.TempDir(), name)
	if len(candidate) < 100 {
		return candidate
	}
	f, err := os.CreateTemp(".", "sock-*.sock")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

func TestPeerCredentialListenerExposesKernelPeer(t *testing.T) {
	socket := shortUnixSocket(t, "peer.sock")
	raw, err := net.Listen("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	listener := wrapPeerCredentialListener(raw)
	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()
	conn, err := net.Dial("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	var serverConn net.Conn
	select {
	case serverConn = <-accepted:
	case err := <-acceptErr:
		t.Fatalf("accept failed: %v", err)
	}
	t.Cleanup(func() { _ = serverConn.Close() })
	peer, err := sdk.ParseUnixPeer(serverConn.RemoteAddr().String())
	require.NoError(t, err)
	require.Equal(t, uint32(os.Geteuid()), peer.UID)
	require.Equal(t, int32(os.Getpid()), peer.PID)
}
