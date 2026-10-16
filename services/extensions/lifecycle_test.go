// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"

	"github.com/stretchr/testify/require"
)

func startLifecycleManager(t *testing.T, root string, requiredIDs ...string) *Manager {
	t.Helper()
	m := NewManager(root, requiredIDs...)
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.NoError(t, m.Start(context.Background()))
	waitStartupFinished(t, m)
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func lifecycleResult(t *testing.T, result packages.LifecycleResult) packages.LifecycleResult {
	t.Helper()
	require.NoError(t, packages.ValidateLifecycleResult(result))
	return result
}

func TestLiveDisableReenable(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	setDesiredState(t, root, "sample", true)
	m := startLifecycleManager(t, root)

	observed := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, observed.State)
	require.Equal(t, "1.0", observed.ObservedVersion)

	// The running manager owns the package lock: concurrent stopped-host
	// mutation stays refused while live operations proceed.
	if lock, err := packages.AcquirePackageLock(root); err == nil {
		_ = lock.Close()
		t.Fatal("package lock was available while manager was running")
	}

	disabled := lifecycleResult(t, m.Deactivate(context.Background(), packages.LifecycleRequest{PackageID: "sample", Operation: sdk.LifecycleDeactivate}))
	require.Equal(t, packages.LifecycleDisabled, disabled.State)
	_, _, ok := m.Lookup("sample")
	require.False(t, ok, "disabled package must close browser admission")

	// The durable requested state is the intent, not a marker file: the
	// definite disable clears any interruption marker.
	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.False(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)

	enabled := lifecycleResult(t, m.Activate(context.Background(), packages.LifecycleRequest{PackageID: "sample", Operation: sdk.LifecycleActivate}))
	require.Equal(t, packages.LifecycleEnabled, enabled.State)
	_, _, ok = m.Lookup("sample")
	require.True(t, ok, "re-enabled package must serve browser admission again")
	intent, err = packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.True(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
}

func TestDeactivateClosesAdmissionBeforeTeardown(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	setDesiredState(t, root, "sample", true)
	m := startLifecycleManager(t, root)

	// External teardown moved to the required controller (F03): disable
	// still closes ordinary admission and navigation first, then stops
	// the backend, then clears the interruption marker durably.
	result := lifecycleResult(t, m.Deactivate(context.Background(), packages.LifecycleRequest{PackageID: "sample", Operation: sdk.LifecycleDeactivate}))
	require.Equal(t, packages.LifecycleDisabled, result.State)
	_, _, ok := m.Lookup("sample")
	require.False(t, ok, "disable must close admission")
	require.Empty(t, m.List(), "disable must close navigation")
	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.False(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
}

func TestDeactivateAfterBackendLoss(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	setDesiredState(t, root, "sample", true)
	m := startLifecycleManager(t, root)

	// The backend process dies outside manager control. Lookup reaps the
	// crash synchronously; disable then verifies the absent backend as
	// stopped instead of blocking cleanup.
	m.mu.Lock()
	item := m.running["sample"]
	require.NotNil(t, item)
	item.client.Kill()
	m.mu.Unlock()
	_, _, ok := m.Lookup("sample")
	require.False(t, ok, "crashed backend must lose admission")
	result := lifecycleResult(t, m.Deactivate(context.Background(), packages.LifecycleRequest{PackageID: "sample", Operation: sdk.LifecycleDeactivate}))
	require.Equal(t, packages.LifecycleDisabled, result.State)
	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.False(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
}

func TestRequiredPackageIsolation(t *testing.T) {
	root := shortTestRoot(t)
	installCatalogPackage(t, root, testCatalogEntry("soda", "1.0", true, ""), nil)
	installCatalogPackage(t, root, testCatalogEntry("optional", "1.0", false, ""), nil)
	setDesiredState(t, root, "optional", true)
	m := NewManager(root, "soda")
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.NoError(t, m.SetCallbackHandlerFactory(func(string) http.Handler { return http.NotFoundHandler() }))
	configureTestCatalog(t, m, root, testCatalogEntry("soda", "1.0", true, ""), testCatalogEntry("optional", "1.0", false, ""))
	require.NoError(t, m.Start(context.Background()))
	waitStartupFinished(t, m)
	t.Cleanup(func() { _ = m.Close() })

	refused := lifecycleResult(t, m.Deactivate(context.Background(), packages.LifecycleRequest{PackageID: "soda", Operation: sdk.LifecycleDeactivate}))
	require.Equal(t, packages.LifecycleFailed, refused.State)
	require.Equal(t, packages.LifecycleErrRequiredPackage, refused.Error.Code)
	_, _, ok := m.Lookup("soda")
	require.True(t, ok, "required package must keep running after refused disable")

	// Optional churn never disturbs the required package.
	require.Equal(t, packages.LifecycleDisabled, m.Deactivate(context.Background(), packages.LifecycleRequest{PackageID: "optional", Operation: sdk.LifecycleDeactivate}).State)
	_, _, ok = m.Lookup("soda")
	require.True(t, ok, "required package must survive optional disable")
	require.Equal(t, packages.LifecycleEnabled, m.Activate(context.Background(), packages.LifecycleRequest{PackageID: "optional", Operation: sdk.LifecycleActivate}).State)
	_, _, ok = m.Lookup("soda")
	require.True(t, ok, "required package must survive optional re-enable")

	snapshot := m.Snapshot()
	require.Len(t, snapshot, 2)
	require.Equal(t, "optional", snapshot[0].ID)
	require.False(t, snapshot[0].Required)
	require.Equal(t, "soda", snapshot[1].ID)
	require.True(t, snapshot[1].Required)
	require.NotEmpty(t, snapshot[1].InstallationID)

	require.Equal(t, packages.LifecycleDisabled, m.Deactivate(context.Background(), packages.LifecycleRequest{PackageID: "optional", Operation: sdk.LifecycleDeactivate}).State)
	snapshot = m.Snapshot()
	require.Len(t, snapshot, 1)
	require.Equal(t, "soda", snapshot[0].ID)
}

func TestActivateValidation(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	setDesiredState(t, root, "sample", true)
	m := startLifecycleManager(t, root)

	mismatch := lifecycleResult(t, m.Activate(context.Background(), packages.LifecycleRequest{PackageID: "sample", PackageVersion: "9.9", Operation: sdk.LifecycleActivate}))
	require.Equal(t, packages.LifecycleFailed, mismatch.State)
	require.Equal(t, packages.LifecycleErrVersionMismatch, mismatch.Error.Code)

	missing := lifecycleResult(t, m.Activate(context.Background(), packages.LifecycleRequest{PackageID: "ghost", Operation: sdk.LifecycleActivate}))
	require.Equal(t, packages.LifecycleFailed, missing.State)
	require.Equal(t, packages.LifecycleErrNotInstalled, missing.Error.Code)

	invalid := m.Activate(context.Background(), packages.LifecycleRequest{PackageID: "../escape", Operation: sdk.LifecycleActivate})
	require.Equal(t, packages.LifecycleFailed, invalid.State)

	// Activating the running version is idempotent.
	again := lifecycleResult(t, m.Activate(context.Background(), packages.LifecycleRequest{PackageID: "sample", PackageVersion: "1.0", Operation: sdk.LifecycleActivate}))
	require.Equal(t, packages.LifecycleEnabled, again.State)
}

func TestInterruptedDeactivateReobservedOnRestart(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	_, err := packages.EnsureInstallation(root, "sample")
	require.NoError(t, err)
	primer := NewManager(root)
	primer.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.NoError(t, primer.Start(context.Background()))
	waitStartupFinished(t, primer)
	installation, err := packages.LoadInstallation(root, "sample")
	require.NoError(t, err)
	require.NoError(t, primer.Close())
	// Crash between requested-state persistence and effects: the intent
	// says disabled with a pending deactivation.
	pending := packages.IntentDeactivate
	_, err = packages.StoreIntent(root, "sample", installation, false, &pending, "0")
	require.NoError(t, err)
	restarted := NewManager(root)
	restarted.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.NoError(t, restarted.Start(context.Background()))
	waitStartupFinished(t, restarted)
	t.Cleanup(func() { _ = restarted.Close() })
	// No effect is retried blindly: the package stays stopped and the
	// definite disabled outcome finalizes the intent.
	_, _, ok := restarted.Lookup("sample")
	require.False(t, ok)
	observed := lifecycleResult(t, restarted.Observe("sample"))
	require.Equal(t, packages.LifecycleDisabled, observed.State)
	require.False(t, observed.Unconfirmed)
	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.False(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
	// The package remains usable afterwards.
	require.Equal(t, packages.LifecycleEnabled, restarted.Activate(context.Background(), packages.LifecycleRequest{PackageID: "sample", Operation: sdk.LifecycleActivate}).State)
}

func TestInterruptedActivateReobservedOnRestart(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	_, err := packages.EnsureInstallation(root, "sample")
	require.NoError(t, err)
	primer := NewManager(root)
	primer.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.NoError(t, primer.Start(context.Background()))
	waitStartupFinished(t, primer)
	installation, err := packages.LoadInstallation(root, "sample")
	require.NoError(t, err)
	require.NoError(t, primer.Close())
	// Crash after requested-state persistence before the backend started:
	// the intent says enabled with a pending activation.
	pending := packages.IntentActivate
	_, err = packages.StoreIntent(root, "sample", installation, true, &pending, "0")
	require.NoError(t, err)
	restarted := NewManager(root)
	restarted.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.NoError(t, restarted.Start(context.Background()))
	waitStartupFinished(t, restarted)
	t.Cleanup(func() { _ = restarted.Close() })
	_, _, ok := restarted.Lookup("sample")
	require.False(t, ok, "interrupted enable must wait for explicit retry")
	observed := lifecycleResult(t, restarted.Observe("sample"))
	require.Equal(t, packages.LifecycleFailed, observed.State)
	require.True(t, observed.Unconfirmed)
	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation)
	require.NotNil(t, intent.PendingOperation)
}

// An uncontrolled completion-write failure keeps the fence on the
// retained backend: no routing, no usable token, pending preserved.
// An explicit retry after storage recovers completes on the same
// instance with the same token.
func TestUncontrolledPersistFailureKeepsFence(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	// Slow the backend start so the storage break below lands
	// deterministically between registration and completion.
	knobData, err := json.Marshal(testFixtureKnobs{SlowStartMs: 3000})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".data", "sample"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".data", "sample", "fixture.json"), knobData, 0o600))
	setDesiredState(t, root, "sample", false)
	m := startLifecycleManager(t, root)

	// Prime the interruption marker, then break the completion write
	// while the slow backend starts.
	primed := make(chan struct{})
	go func() {
		defer close(primed)
		for {
			intent, err := packages.LoadIntent(root, "sample")
			if err == nil && intent.PendingOperation != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	activated := make(chan packages.LifecycleResult, 1)
	go func() {
		activated <- m.Activate(context.Background(), packages.LifecycleRequest{PackageID: "sample", Operation: sdk.LifecycleActivate})
	}()
	<-primed
	intentPath := filepath.Join(root, ".intents", "sample.json")
	intentData, err := os.ReadFile(intentPath)
	require.NoError(t, err)
	require.NoError(t, os.Remove(intentPath))
	require.NoError(t, os.Mkdir(intentPath, 0o700))
	restore := func() {
		_ = os.RemoveAll(intentPath)
		_ = os.WriteFile(intentPath, intentData, 0o600)
	}
	t.Cleanup(restore)
	result := lifecycleResult(t, <-activated)
	require.Equal(t, packages.LifecycleFailed, result.State)
	require.True(t, result.Unconfirmed)
	require.Equal(t, packages.LifecycleErrUnavailable, result.Error.Code)
	_, _, ok := m.Lookup("sample")
	require.False(t, ok, "unpersisted completion must keep admission closed")
	require.Empty(t, m.List(), "unpersisted completion must keep navigation closed")
	m.mu.Lock()
	item := m.running["sample"]
	m.mu.Unlock()
	require.NotNil(t, item, "backend is retained but fenced")
	token, err := m.IssueRuntimeAdmission("sample", item.installation, item.descriptor.InstanceID, []string{"native.actor.read"})
	require.NoError(t, err)
	_, ok = m.VerifyBackgroundAdmission(token)
	require.False(t, ok, "fenced tokens must not verify")
	restore()
	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.True(t, intent.DesiredEnabled)
	require.NotNil(t, intent.PendingOperation)
	// Explicit retry through the admin mutation path completes on
	// the retained instance; the same token works without
	// redelivery. The synchronous Activate entry stays refused while
	// the failure fence is held.
	refused := m.Activate(context.Background(), packages.LifecycleRequest{PackageID: "sample", Operation: sdk.LifecycleActivate})
	require.Equal(t, packages.LifecycleFailed, refused.State)
	require.Equal(t, packages.LifecycleErrInProgress, refused.Error.Code)
	requestEnabledAccept(t, m, "sample", intent.Generation, m.catalogSHA, true)
	waitActiveOpDone(t, m)
	enabled := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, enabled.State)
	require.False(t, enabled.Unconfirmed)
	_, _, ok = m.Lookup("sample")
	require.True(t, ok, "retry must reopen admission")
	_, ok = m.VerifyBackgroundAdmission(token)
	require.True(t, ok, "retained token must work after retry completes")
}

func TestInstallAndActivateReportsDownloadFailure(t *testing.T) {
	root := shortTestRoot(t)
	m := startLifecycleManager(t, root)
	entry := sdk.CatalogPackage{
		ID: "tailnet", Version: "1.2.3", URL: "https://127.0.0.1:1/tailnet.tar.gz",
		Size: 1024, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		FountainProto: sdk.Protocol, BrowserContract: sdk.BrowserContractVersion,
	}
	result := lifecycleResult(t, m.InstallAndActivate(context.Background(), entry, false))
	require.Equal(t, packages.LifecycleFailed, result.State)
	require.True(t, result.Unconfirmed)
	require.Equal(t, packages.LifecycleErrAcquireFailed, result.Error.Code)
	_, err := os.Lstat(filepath.Join(root, "tailnet"))
	require.True(t, os.IsNotExist(err), "failed acquisition must install nothing")
}
