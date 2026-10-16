// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"

	"github.com/stretchr/testify/require"
)

// startFX04Manager starts one manager over root with the given
// required IDs and catalog entries, spawning the test helper binary
// for backends. The catalog URL is unreachable so any acquisition
// attempt fails fast: success proves zero downloads.
func startFX04Manager(t *testing.T, root string, requiredIDs []string, entries ...sdk.CatalogPackage) *Manager {
	t.Helper()
	m := NewManager(root, requiredIDs...)
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	configureTestCatalog(t, m, root, entries...)
	require.NoError(t, m.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return testAuthorizeMux(m, instanceID)
	}))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })
	return m
}

// FX04-9: startup restores an interrupted required swap before
// validation: the crash between the two renames leaves an absent
// target whose retained backup reboots the required package offline
// with zero downloads, preserving installation UUID and data.
func TestFX04StartupRestoresInterruptedRequiredSwap(t *testing.T) {
	root := shortTestRoot(t)
	installLifecyclePackage(t, root, "controller", controllerManifest, testFixtureKnobs{Role: "controller"})
	entry := testCatalogEntry("controller", "1.0.0", true, "")
	entry.Capabilities = []string{"native.extension.lifecycle"}
	entry.URL = "https://127.0.0.1:1/unreachable.tar.gz"
	require.NoError(t, packages.WriteReceipt(filepath.Join(root, "controller"), entry))

	m := startFX04Manager(t, root, []string{"controller"}, entry)
	_, _, ok := m.Lookup("controller")
	require.True(t, ok, "required controller must be ready at Start return")
	before, err := packages.LoadInstallation(root, "controller")
	require.NoError(t, err)
	require.NoError(t, m.Close())

	// Crash between the two renames: old bytes sit in the fixed
	// backup while the target is absent.
	require.NoError(t, os.Rename(filepath.Join(root, "controller"), filepath.Join(root, ".previous-controller")))

	m = startFX04Manager(t, root, []string{"controller"}, entry)
	_, _, ok = m.Lookup("controller")
	require.True(t, ok, "retained backup must reboot the required package with no network")
	require.NoFileExists(t, filepath.Join(root, ".previous-controller"))
	after, err := packages.LoadInstallation(root, "controller")
	require.NoError(t, err)
	require.Equal(t, before, after, "repair preserves installation identity")
	require.FileExists(t, filepath.Join(root, ".data", "controller", "fixture.json"), "repair preserves private data")
}

// FX04-10: an enabled optional backup restores at startup and starts
// without acquisition: the unreachable catalog URL proves no
// download ran on the boot path.
func TestFX04StartupRestoresEnabledOptionalWithoutAcquisition(t *testing.T) {
	root := shortTestRoot(t)
	entry := testCatalogEntry("sample", "1.0.0", false, "")
	entry.Capabilities = []string{"native.actor.read"}
	installCatalogPackage(t, root, entry, nil)
	persistDesiredState(t, root, "sample", true)
	before, err := packages.LoadInstallation(root, "sample")
	require.NoError(t, err)

	m := startFX04Manager(t, root, nil, entry)
	waitStartupFinished(t, m)
	_, _, ok := m.Lookup("sample")
	require.True(t, ok, "restored optional backend must run after the startup pass")
	require.NoFileExists(t, filepath.Join(root, ".previous-sample"))
	after, err := packages.LoadInstallation(root, "sample")
	require.NoError(t, err)
	require.Equal(t, before, after, "repair preserves installation identity")
	observed := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, observed.State)
	require.False(t, observed.Unconfirmed)
}

// FX04-11: an optional package whose repair fails is isolated at
// startup: core starts, the unrepairable bytes are never validated
// or executed, and the row keeps a visible failure for an explicit
// retry after repair. No helper process runs anywhere here.
func TestFX04OptionalRepairFailureIsolatesWithoutExecution(t *testing.T) {
	root := shortTestRoot(t)
	writeClaimFixture(t, root, "sample", "1.0.0", "")
	seedIntent(t, root, "sample", true)
	// The crash left an unverified target beside an invalid backup:
	// neither carries a generated receipt, so repair must refuse.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".previous-sample"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".previous-sample", "junk"), []byte("not a package"), 0o600))

	m := NewManager(root)
	configureTestCatalog(t, m, root, testCatalogEntry("sample", "1.0.0", false, ""))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })

	require.True(t, m.recoveryIsolated("sample"), "failed optional repair must isolate the package")
	m.mu.Lock()
	require.Empty(t, m.running, "isolated bytes must never execute")
	m.mu.Unlock()
	_, _, ok := m.Lookup("sample")
	require.False(t, ok, "isolated package serves nothing")
	observed := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleFailed, observed.State)
	require.True(t, observed.Unconfirmed, "desired but stopped with unrepairable bytes stays unconfirmed, never enabled")
	failure := m.lastFailure("sample")
	require.NotNil(t, failure, "startup must leave the precise repair failure visible")
	require.Equal(t, packages.LifecycleErrUnavailable, failure.Code)

	// An explicit reconcile pass still refuses to execute the bytes.
	require.True(t, m.reconcileStablePass(context.Background(), true, make(map[string]bool)))
	m.mu.Lock()
	require.Empty(t, m.running, "reconcile must never execute isolated bytes")
	m.mu.Unlock()
	require.DirExists(t, filepath.Join(root, "sample"), "refused bytes stay for inspection")
	require.DirExists(t, filepath.Join(root, ".previous-sample"), "refused backup stays for inspection")
}

// FX04-12: a required package whose repair fails blocks required
// readiness: Start refuses before any validation, acquisition or
// backend start. No helper process runs anywhere here.
func TestFX04RequiredRepairFailureBlocksReadiness(t *testing.T) {
	root := shortTestRoot(t)
	// The crash left an unverified target beside an invalid backup:
	// neither carries a generated receipt, so repair must refuse.
	writeClaimFixture(t, root, "controller", "1.0.0", "")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".previous-controller"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".previous-controller", "junk"), []byte("not a package"), 0o600))

	m := NewManager(root, "controller")
	configureTestCatalog(t, m, root, testCatalogEntry("controller", "1.0.0", true, ""))
	err := m.Start(context.Background())
	require.ErrorContains(t, err, "package repair failed")
	require.ErrorContains(t, err, "controller")
	m.mu.Lock()
	require.Empty(t, m.running, "blocked readiness starts nothing")
	m.mu.Unlock()
}

// FX04-13: restored older bytes fail current catalog admission. The
// repair brings back the retained version, but required bootstrap
// still refuses the obsolete package instead of authorizing it.
func TestFX04RestoredWrongVersionFailsAdmission(t *testing.T) {
	root := shortTestRoot(t)
	installLifecyclePackage(t, root, "controller", controllerManifest, testFixtureKnobs{Role: "controller"})
	old := testCatalogEntry("controller", "1.0.0", true, "")
	old.Capabilities = []string{"native.extension.lifecycle"}
	require.NoError(t, packages.WriteReceipt(filepath.Join(root, "controller"), old))
	require.NoError(t, os.Rename(filepath.Join(root, "controller"), filepath.Join(root, ".previous-controller")))

	current := testCatalogEntry("controller", "2.0.0", true, "")
	current.Capabilities = []string{"native.extension.lifecycle"}
	current.URL = "https://127.0.0.1:1/unreachable.tar.gz"
	m := NewManager(root, "controller")
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	configureTestCatalog(t, m, root, current)
	err := m.Start(context.Background())
	require.ErrorContains(t, err, "bootstrap failed", "obsolete restored bytes must not satisfy the current catalog")
	m.mu.Lock()
	require.Empty(t, m.running, "obsolete bytes never start")
	m.mu.Unlock()
	require.DirExists(t, filepath.Join(root, "controller"), "retained bytes stay for inspection")
}

// FX04-14: standalone layouts without a managed catalog recover too.
// Discovery falls back to intent and installation ownership, and the
// restored backend starts without any catalog default.
func TestFX04StandaloneRecoveryWithoutCatalog(t *testing.T) {
	root := shortTestRoot(t)
	manifest := `{"protocol":1,"id":"solo","name":"Test","version":"1.0.0","executable":"extension","capabilities":["native.actor.read"]}`
	installLifecyclePackage(t, root, "solo", manifest, testFixtureKnobs{})
	// A previously verified install keeps its generated receipt even
	// when the layout later runs standalone with no catalog: repair
	// binds that receipt identity to the exact ID.
	require.NoError(t, packages.WriteReceipt(filepath.Join(root, "solo"), sdk.CatalogPackage{ID: "solo", Version: "1.0.0", SHA256: strings.Repeat("e", 64)}))
	persistDesiredState(t, root, "solo", true)
	require.NoError(t, os.Rename(filepath.Join(root, "solo"), filepath.Join(root, ".previous-solo")))

	m := NewManager(root)
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.NoError(t, m.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return testAuthorizeMux(m, instanceID)
	}))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })

	waitStartupFinished(t, m)
	_, _, ok := m.Lookup("solo")
	require.True(t, ok, "standalone restore must run after the startup pass")
	require.NoFileExists(t, filepath.Join(root, ".previous-solo"))
}
