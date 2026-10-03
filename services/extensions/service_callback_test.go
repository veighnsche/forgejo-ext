// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	packages "forgejo.org/modules/extensions"

	"github.com/stretchr/testify/require"
)

func TestServiceCallbackLifecycle(t *testing.T) {
	root := shortTestRoot(t)
	path := filepath.Join(root, "callback.sock")
	m := NewManager(filepath.Join(root, "packages"))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "service")
	})
	require.NoError(t, m.SetServiceCallbackEndpoint(path, handler))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equal(t, os.ModeSocket, info.Mode()&os.ModeSocket)
	require.Equal(t, os.FileMode(0o660), info.Mode().Perm())
	require.Error(t, m.SetServiceCallbackEndpoint(path, handler))
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	response, err := (&http.Client{Transport: transport}).Get("http://service/callback")
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, response.Body.Close())
	require.NoError(t, err)
	require.Equal(t, "service", string(body))
	require.NoError(t, m.Close())
	_, err = os.Lstat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, m.Start(context.Background()))
	// A replacement at the configured path belongs to its creator, not to this
	// listener. Close must not remove it via Go's automatic Unix socket cleanup.
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.WriteFile(path, []byte("replacement"), 0o600))
	require.NoError(t, m.Close())
	body, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(body))
}

func TestServiceCallbackRejectsUnavailableConfiguration(t *testing.T) {
	for _, kind := range []string{"relative", "unclean", "missing-parent", "existing-file", "existing-symlink", "existing-socket", "nil-handler"} {
		t.Run(kind, func(t *testing.T) {
			root := shortTestRoot(t)
			path := filepath.Join(root, "callback.sock")
			var handler http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
			switch kind {
			case "relative":
				path = "callback.sock"
			case "unclean":
				path = root + "/./callback.sock"
			case "missing-parent":
				path = filepath.Join(root, "absent", "callback.sock")
			case "existing-file":
				require.NoError(t, os.WriteFile(path, []byte("owned"), 0o600))
			case "existing-symlink":
				require.NoError(t, os.Symlink(filepath.Join(root, "absent"), path))
			case "existing-socket":
				listener, err := net.Listen("unix", path)
				require.NoError(t, err)
				t.Cleanup(func() { _ = listener.Close() })
			case "nil-handler":
				handler = nil
			}
			before, _ := os.Lstat(path)
			m := NewManager(filepath.Join(root, "packages"))
			require.NoError(t, m.SetServiceCallbackEndpoint(path, handler))
			require.Error(t, m.Start(context.Background()))
			if before != nil {
				after, err := os.Lstat(path)
				require.NoError(t, err)
				require.True(t, os.SameFile(before, after))
			}
			lock, err := packages.AcquirePackageLock(m.root)
			require.NoError(t, err)
			require.NoError(t, lock.Close())
		})
	}
}

func TestServiceBridgeRequiresConfiguredEndpoint(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	manifest := `{"protocol":1,"id":"sample","name":"Test","version":"1.0","executable":"extension","capabilities":["native.service.bridge","native.actor.read"]}`
	require.NoError(t, os.WriteFile(filepath.Join(root, "sample", "extension.json"), []byte(manifest), 0o600))
	m := NewManager(root)
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.ErrorContains(t, m.Start(context.Background()), "service callback configuration failed")
	path := filepath.Join(root, "callback.sock")
	require.NoError(t, m.SetServiceCallbackEndpoint(path, http.NotFoundHandler()))
	require.NoError(t, m.SetCallbackHandlerFactory(func(string) http.Handler { return http.NotFoundHandler() }))
	require.NoError(t, m.Start(context.Background()))
	privateSocket, err := os.Lstat(filepath.Join(m.running["sample"].runtimeDir, "c"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), privateSocket.Mode().Perm())
	require.NoError(t, m.Close())
}

func TestServiceCallbackReclaimsStaleSocket(t *testing.T) {
	root := shortTestRoot(t)
	path := filepath.Join(root, "callback.sock")
	// Simulate an unclean shutdown: bound socket file left behind with no
	// listener. See SetUnlinkOnClose(false) in startServiceCallback.
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	require.NoError(t, err)
	stale.SetUnlinkOnClose(false)
	require.NoError(t, stale.Close())
	_, err = os.Lstat(path)
	require.NoError(t, err)
	m := NewManager(filepath.Join(root, "packages"))
	require.NoError(t, m.SetServiceCallbackEndpoint(path, http.NotFoundHandler()))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equal(t, os.ModeSocket, info.Mode().Type())
	require.NoError(t, m.Close())
}

func TestServiceCallbackStartupFailureCleansOwnedSocket(t *testing.T) {
	root := shortTestRoot(t)
	packageRoot := filepath.Join(root, "packages")
	require.NoError(t, os.MkdirAll(filepath.Join(packageRoot, "broken"), 0o700))
	path := filepath.Join(root, "callback.sock")
	m := NewManager(packageRoot)
	require.NoError(t, m.SetServiceCallbackEndpoint(path, http.NotFoundHandler()))
	require.Error(t, m.Start(context.Background()))
	_, err := os.Lstat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}
