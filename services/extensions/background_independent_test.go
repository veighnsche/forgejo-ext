// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"

	"github.com/stretchr/testify/require"
)

// sha256Path records the exact bytes of a task input or artifact.
func sha256Path(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// buildIndependentConsumer compiles the standalone consumer with the same
// toolchain as this test and returns its path and digest. The parent module's
// replace directive selects the canonical SDK; qualification evidence records
// the source revision and SDK tree separately.
func buildIndependentConsumer(t *testing.T, dir string) (string, string) {
	t.Helper()
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "caller location is required to find the module root")
	pkgDir := filepath.Dir(thisFile)
	mainPath := filepath.Join(pkgDir, "testdata", "r03-consumer", "main.go")
	before := sha256Path(t, mainPath)
	binary := filepath.Join(dir, "r03-consumer")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "build", "-o", binary, "./services/extensions/testdata/r03-consumer")
	cmd.WaitDelay = time.Second
	cmd.Dir = filepath.Join(pkgDir, "..", "..")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=-mod=readonly")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "independent consumer build failed: %s", output)
	require.Equal(t, before, sha256Path(t, mainPath), "consumer inputs must be unchanged by the build")
	return binary, sha256Path(t, binary)
}

// installIndependentPackage stages one package whose executable is the
// independently built consumer binary, never the re-executed test binary.
func installIndependentPackage(t *testing.T, root, id, binary string) {
	t.Helper()
	packageDir := filepath.Join(root, id)
	require.NoError(t, os.MkdirAll(packageDir, 0o700))
	data, err := os.ReadFile(binary)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension"), data, 0o700))
	manifest := fmt.Sprintf(`{"protocol":1,"id":%q,"name":"R03Independent","version":"1.0","executable":"extension","capabilities":["native.service.bridge","native.background.operations"]}`, id)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(manifest), 0o600))
}

// startIndependentManager starts the real extension manager over the
// independently built consumer. No test-helper environment is installed:
// the child runs without any test scaffolding.
func startIndependentManager(t *testing.T, root, binary string) (*Manager, uint32) {
	t.Helper()
	installIndependentPackage(t, root, "r03consumer", binary)
	setDesiredState(t, root, "r03consumer", true)
	manager := NewManager(root)
	require.NoError(t, manager.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	}))
	require.NoError(t, manager.SetServiceCallbackEndpoint(filepath.Join(root, "service.sock"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	self := uint32(os.Geteuid())
	require.NoError(t, manager.SetServiceBridgePeers(map[uint32]string{self: "r03consumer"}))
	require.NoError(t, manager.Start(context.Background()))
	waitStartupFinished(t, manager)
	t.Cleanup(func() { _ = manager.Close() })
	return manager, self
}

// TestIndependentConsumerBackgroundAdmission runs the independently built
// consumer through the real extension manager/package handshake: admission,
// rotation, runtime restart, and stale-admission refusal.
func TestIndependentConsumerBackgroundAdmission(t *testing.T) {
	binary, digest := buildIndependentConsumer(t, t.TempDir())
	t.Logf("independent consumer binary sha256=%s", digest)
	root := shortTestRoot(t)
	manager, self := startIndependentManager(t, root, binary)
	installation, err := packages.LoadInstallation(root, "r03consumer")
	require.NoError(t, err)
	require.NotEmpty(t, installation)

	// The manager bootstraps only a live, non-exited child, so this
	// admission proves the independent binary completed the real handshake.
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

	// Wrong peer and forged installation are rejected.
	_, _, err = manager.BootstrapServiceAdmission(self^0xffffff, "")
	require.ErrorContains(t, err, "not permitted")
	_, _, err = manager.BootstrapServiceAdmission(self, "22222222-2222-4333-8444-555555555555")
	require.ErrorContains(t, err, "installation mismatch")

	// Restart preserves installation identity while replacing admission.
	require.NoError(t, manager.Close())
	_, ok = manager.VerifyBackgroundAdmission(replacement)
	require.False(t, ok, "runtime stop must revoke service admissions")
	require.NoError(t, manager.Start(context.Background()))
	waitStartupFinished(t, manager)
	preserved, err := packages.LoadInstallation(root, "r03consumer")
	require.NoError(t, err)
	require.Equal(t, installation, preserved)
	fresh, _, err := manager.BootstrapServiceAdmission(self, "")
	require.NoError(t, err)
	require.NotEqual(t, replacement, fresh)
	_, ok = manager.VerifyBackgroundAdmission(replacement)
	require.False(t, ok, "restart must invalidate previous admissions")
	freshClaims, ok := manager.VerifyBackgroundAdmission(fresh)
	require.True(t, ok, "newly admitted consumer succeeds")
	require.Equal(t, installation, freshClaims.InstallationID)
}
