// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"

	"github.com/stretchr/testify/require"
)

// acceptMutation requires immediate acceptance of an async admin
// mutation: validation, reservation, and durable persistence all
// happened before any effect, so the in-progress state with no error
// is the redirect-equivalent contract.
func acceptMutation(t *testing.T, result packages.LifecycleResult, operation, state string) packages.LifecycleResult {
	t.Helper()
	require.NoError(t, packages.ValidateLifecycleResult(result))
	require.Equal(t, operation, result.Operation)
	require.Equal(t, state, result.State)
	require.Nil(t, result.Error)
	return result
}

// requestEnabledAccept adopts one async mutation, retrying briefly
// while a startup reconciliation attempt owns the single slot.
func requestEnabledAccept(t *testing.T, m *Manager, id, generation, sha string, enabled bool) packages.LifecycleResult {
	t.Helper()
	operation := sdk.LifecycleActivate
	state := packages.LifecycleActivating
	if !enabled {
		operation = sdk.LifecycleDeactivate
		state = packages.LifecycleDeactivating
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		result := m.RequestEnabled(context.Background(), id, generation, sha, enabled)
		if result.Error != nil && result.Error.Code == packages.LifecycleErrInProgress {
			if time.Now().After(deadline) {
				t.Fatalf("mutation for %s never adopted: %v", id, result.Error)
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return acceptMutation(t, result, operation, state)
	}
}

// waitActiveOpDone joins the manager-owned mutation task: it returns
// once no command is active, whether the task already finished or
// finishes while waiting.
func waitActiveOpDone(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for {
		m.mu.Lock()
		active := m.activeOp
		m.mu.Unlock()
		if active == nil {
			return
		}
		select {
		case <-active.done:
			return
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("active mutation never finished")
		}
	}
}

// waitObservePolls local observation until it reports the wanted state
// (or timeout): the settled outcome of an async mutation.
func waitObserveState(t *testing.T, m *Manager, id, want string, timeout time.Duration) packages.LifecycleResult {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		observed := m.Observe(id)
		if observed.State == want && !observed.Unconfirmed {
			return observed
		}
		if time.Now().After(deadline) {
			t.Fatalf("observation for %s never settled to %s: %+v", id, want, observed)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// F26-1: exact state reads are coherent local observations with zero
// controller or host callbacks, even while the host checks admission.
func TestF026StateReadsMakeZeroControllerCalls(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller"}, true)
	enabledSample(t, h)
	stateDir := controllerStateDir(h)
	base := readObserveCount(stateDir)
	require.GreaterOrEqual(t, base, 1, "enable must have observed effects at least once")

	for range 3 {
		response, reject := h.m.ControlState(context.Background(), "controller", "sample")
		require.Nil(t, reject)
		require.Equal(t, h.sha, response.CatalogSHA256)
		require.Equal(t, "controller", response.ControllerID)
		require.NotNil(t, response.ControllerInstance)
		require.True(t, response.ControllerReady)
		require.True(t, response.AdmissionOpen, "completed enablement opens ordinary admission")
		require.Equal(t, "sample", response.Entry.PackageID)
		require.Equal(t, "1", response.Entry.Generation)
		require.True(t, response.Entry.DesiredEnabled)
		require.Nil(t, response.Entry.PendingOperation)
		require.NotNil(t, response.Entry.InstalledVersion)
		require.Equal(t, "1.0.0", *response.Entry.InstalledVersion)
		require.NotNil(t, response.Entry.PackageSHA256)
		require.Equal(t, h.sampleEntry.SHA256, *response.Entry.PackageSHA256)
		_, _, ok := h.m.Lookup("sample")
		require.True(t, ok, "host admission check sees the open backend")
		observed := lifecycleResult(t, h.m.Observe("sample"))
		require.Equal(t, packages.LifecycleEnabled, observed.State)
		require.False(t, observed.Unconfirmed)
	}
	require.Equal(t, base, readObserveCount(stateDir),
		"state, snapshot, and observation reads must not call the controller")
}

// F26-1: during accepted activation the exact state fixture reports
// pending with closed admission from local observations alone.
func TestF026StateDuringActivationReportsPending(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller", SlowActivateMs: 3000}, true)
	requestEnabledAccept(t, h.m, "sample", "0", h.sha, true)
	base := readObserveCount(controllerStateDir(h))

	response, reject := h.m.ControlState(context.Background(), "controller", "sample")
	require.Nil(t, reject)
	require.False(t, response.AdmissionOpen, "ordinary admission stays closed during activation")
	require.Equal(t, "sample", response.Entry.PackageID)
	require.Equal(t, "1", response.Entry.Generation)
	require.True(t, response.Entry.DesiredEnabled)
	require.NotNil(t, response.Entry.PendingOperation)
	require.Equal(t, packages.IntentActivate, *response.Entry.PendingOperation)
	require.Equal(t, base, readObserveCount(controllerStateDir(h)),
		"activation state reads must not call the controller")
	waitActiveOpDone(t, h.m)
	settled, reject := h.m.ControlState(context.Background(), "controller", "sample")
	require.Nil(t, reject)
	require.True(t, settled.AdmissionOpen)
	require.Nil(t, settled.Entry.PendingOperation)
}

// F26-3: the accepted admin mutation returns before effects finish:
// acceptance is immediate even with a slow controller, the durable
// intent already carries the interruption marker, status shows
// in-progress, and the operation still completes.
func TestF026DelayedAcceptanceBeforeEffectsFinish(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller", SlowActivateMs: 3000}, true)
	start := time.Now()
	requestEnabledAccept(t, h.m, "sample", "0", h.sha, true)
	require.Less(t, time.Since(start), 2*time.Second,
		"acceptance must return before the 3s activation effect finishes")
	intent, err := packages.LoadIntent(h.root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation)
	require.True(t, intent.DesiredEnabled)
	require.NotNil(t, intent.PendingOperation)
	require.Equal(t, packages.IntentActivate, *intent.PendingOperation)
	inProgress := lifecycleResult(t, h.m.Observe("sample"))
	require.Equal(t, packages.LifecycleActivating, inProgress.State)
	waitActiveOpDone(t, h.m)
	settled := waitObserveState(t, h.m, "sample", packages.LifecycleEnabled, 30*time.Second)
	require.False(t, settled.Unconfirmed)
	require.Equal(t, 1, readActivateCount(controllerStateDir(h)))
}

// F26-3: browser disconnect never cancels accepted work: the bounded
// task runs on the manager lifetime context, not the connection.
func TestF026DisconnectDoesNotCancelAcceptedWork(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller", SlowActivateMs: 1000}, true)
	ctx, cancel := context.WithCancel(context.Background())
	accepted := h.m.RequestEnabled(ctx, "sample", "0", h.sha, true)
	acceptMutation(t, accepted, sdk.LifecycleActivate, packages.LifecycleActivating)
	cancel()
	waitActiveOpDone(t, h.m)
	settled := waitObserveState(t, h.m, "sample", packages.LifecycleEnabled, 30*time.Second)
	require.False(t, settled.Unconfirmed)
	intent, err := packages.LoadIntent(h.root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation)
	require.Nil(t, intent.PendingOperation)
}

// F26-3: Close cancels the active task and joins it before the store
// unlocks: it returns without waiting out the slow effect, the
// interruption marker is retained, no admission opens, the effect
// never completes, and the package lock is free afterwards.
func TestF026CloseCancelsAndJoinsActiveTask(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller", SlowActivateMs: 15000}, true)
	requestEnabledAccept(t, h.m, "sample", "0", h.sha, true)
	start := time.Now()
	require.NoError(t, h.m.Close())
	require.Less(t, time.Since(start), 12*time.Second,
		"Close must cancel the 15s effect instead of waiting it out")
	intent, err := packages.LoadIntent(h.root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation)
	require.True(t, intent.DesiredEnabled)
	require.NotNil(t, intent.PendingOperation, "cancelled work keeps its interruption marker")
	_, _, ok := h.m.Lookup("sample")
	require.False(t, ok, "cancelled enablement opens no admission")
	require.Equal(t, 0, readActivateCount(controllerStateDir(h)),
		"the cancelled activation effect never completed")
	lock, err := packages.AcquirePackageLock(h.root)
	require.NoError(t, err, "the store unlocks only after the task ends")
	require.NoError(t, lock.Close())
	_, reject := h.m.ControlState(context.Background(), "controller", "sample")
	require.NotNil(t, reject, "a closed manager serves no state")
}

// F26-2: a maintenance-deferred enabled target is actually resumed by
// the authenticated synchronous reconcile: grant adoption plus backend
// admission under the same generation, with no package download and no
// generation rewrite. The deferred row is built by restarting with an
// empty effect journal; the marker-directory variant of the same
// deferral is covered where root ownership can be built.
func TestF026DeferredTargetResumesBySyncReconcile(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller"}, true)
	enabledSample(t, h)
	require.NoError(t, os.Remove(filepath.Join(controllerStateDir(h), "grant.json")))
	require.NoError(t, h.m.Close())

	m := NewManager(h.root, "controller")
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.NoError(t, m.SetCatalogPath(filepath.Join(h.root, ".test-catalog.json")))
	require.NoError(t, m.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return testAuthorizeMux(m, instanceID)
	}))
	// Hold the single slot across the restart so the background
	// startup attempts stay inert until the deferred state is
	// established; both entries run the same ordinary boot
	// reconciliation afterwards.
	require.NoError(t, m.acquireMutation("test-guard"))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })
	deferred := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleFailed, deferred.State)
	require.True(t, deferred.Unconfirmed, "running without a current grant is failed, never enabled")
	m.releaseMutation()

	// The authenticated synchronous reconcile resumes the deferred
	// target; a 409 while a startup attempt owns the slot retries.
	var response ControlReconcileResponse
	var reject *ControlError
	reconcileDeadline := time.Now().Add(60 * time.Second)
	for {
		response, reject = m.ControlReconcile(context.Background(), "controller")
		if reject == nil || reject.HTTPStatus != http.StatusConflict {
			break
		}
		if time.Now().After(reconcileDeadline) {
			t.Fatalf("sync reconcile never admitted: %v", reject)
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.Nil(t, reject, "the deferred target resumes through the shared guard")
	require.Equal(t, m.catalogSHA, response.CatalogSHA256)
	byID := make(map[string]ControlReconcileEntry)
	for _, entry := range response.Entries {
		byID[entry.PackageID] = entry
	}
	resumed := byID["sample"]
	require.Equal(t, packages.LifecycleEnabled, resumed.State)
	require.True(t, resumed.AdmissionOpen)
	require.Nil(t, resumed.Error)
	require.Equal(t, "running", resumed.Backend)
	require.Equal(t, "running", resumed.Effects)
	before, err := os.ReadFile(filepath.Join(controllerStateDir(h), "effect.up"))
	require.NoError(t, err)
	require.Equal(t, "1", string(before), "adoption reuses the owned effect generation")
	intent, err := packages.LoadIntent(h.root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation, "reconcile never rewrites the generation")
	require.True(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
	settled := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, settled.State)
	require.False(t, settled.Unconfirmed)
}

// F26-2: a cancelled reconcile budget fails fast with failing rows,
// never invented completion.
func TestF026ReconcileCancelledBudgetFailsFast(t *testing.T) {
	root := t.TempDir()
	m := controlManager(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, reject := m.ControlReconcile(ctx, "soda")
	require.NotNil(t, reject)
	require.Equal(t, http.StatusServiceUnavailable, reject.HTTPStatus)
}

// F26-4: the required package bootstraps through the actual Start
// path with no bundled bytes: exact installed receipts reboot
// offline with zero downloads, and the verified archive cache
// reinstalls a wiped package with no network.
func TestF026RequiredBootstrapOfflineReuse(t *testing.T) {
	root := shortTestRoot(t)
	controllerEntry := testCatalogEntry("controller", "1.0.0", true, "")
	controllerEntry.Capabilities = []string{"native.extension.lifecycle"}
	controllerEntry.URL = "https://127.0.0.1:1/unreachable.tar.gz"
	installLifecyclePackage(t, root, "controller", controllerManifest, testFixtureKnobs{Role: "controller"})
	require.NoError(t, packages.WriteReceipt(filepath.Join(root, "controller"), controllerEntry))

	start := func() *Manager {
		m := NewManager(root, "controller")
		m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
		configureTestCatalog(t, m, root, controllerEntry)
		require.NoError(t, m.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
			return testAuthorizeMux(m, instanceID)
		}))
		require.NoError(t, m.Start(context.Background()))
		return m
	}
	m := start()
	_, _, ok := m.Lookup("controller")
	require.True(t, ok, "required controller must be ready at Start return")
	before, err := packages.LoadInstallation(root, "controller")
	require.NoError(t, err)
	require.NoError(t, m.Close())

	// Offline reboot reuses the exact receipt with no acquisition.
	m = start()
	t.Cleanup(func() { _ = m.Close() })
	_, _, ok = m.Lookup("controller")
	require.True(t, ok, "cached required bytes reboot offline")
	after, err := packages.LoadInstallation(root, "controller")
	require.NoError(t, err)
	require.Equal(t, before, after, "bootstrap preserves installation identity")
	require.NoError(t, m.Close())

	// A wiped package reinstalls from the verified cache with the
	// same unreachable URL proving no download ran.
	executable, err := os.Executable()
	require.NoError(t, err)
	backend, err := os.ReadFile(executable)
	require.NoError(t, err)
	archive := controlTarGzip(t, map[string][]byte{
		"extension.json": []byte(`{"protocol":1,"id":"controller","name":"Test","version":"1.0.0","executable":"backend","capabilities":["native.extension.lifecycle"]}`),
		"backend":        backend,
	})
	cachedEntry := controllerEntry
	cachedEntry.SHA256 = archiveSHAHex(t, archive)
	cachedEntry.Size = int64(len(archive))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".cache", "packages"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".cache", "packages", cachedEntry.SHA256+".tar.gz"), archive, 0o600))
	require.NoError(t, os.RemoveAll(filepath.Join(root, "controller")))
	m = NewManager(root, "controller")
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	configureTestCatalog(t, m, root, cachedEntry)
	require.NoError(t, m.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return testAuthorizeMux(m, instanceID)
	}))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })
	_, _, ok = m.Lookup("controller")
	require.True(t, ok, "verified cache reinstalls the required package with no network")
	require.NoError(t, packages.ValidateInstalled(root, cachedEntry))
	cached, err := packages.LoadInstallation(root, "controller")
	require.NoError(t, err)
	require.Equal(t, before, cached, "cache reinstall preserves installation identity")
}

func archiveSHAHex(t *testing.T, archive []byte) string {
	t.Helper()
	sum := sha256.Sum256(archive)
	return hex.EncodeToString(sum[:])
}

// F26-4: core is ready before a delayed optional socket, and an
// optional failure is isolated: the required controller serves from
// Start return. An explicit bounded reconcile after a later crash restores
// the optional backend with adoption, never a persistent polling loop.
func TestF026OptionalCrashReconcilesCoreUndisturbed(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller"}, true)
	h.m.mu.Lock()
	controllerInstance := h.m.controllers["controller"].instanceID
	h.m.mu.Unlock()
	require.NotEmpty(t, controllerInstance)
	_, _, ok := h.m.Lookup("controller")
	require.True(t, ok, "required core is ready at Start return")
	_, _, ok = h.m.Lookup("sample")
	require.False(t, ok, "nothing optional runs before enablement")

	enabledSample(t, h)
	h.m.mu.Lock()
	item := h.m.running["sample"]
	require.NotNil(t, item)
	item.client.Kill()
	h.m.mu.Unlock()
	_, _, ok = h.m.Lookup("sample")
	require.False(t, ok, "crashed backend loses admission")

	_, reject := h.m.ControlReconcile(context.Background(), "controller")
	require.Nil(t, reject)
	restored := waitObserveState(t, h.m, "sample", packages.LifecycleEnabled, 60*time.Second)
	require.False(t, restored.Unconfirmed)
	h.m.mu.Lock()
	require.Equal(t, controllerInstance, h.m.controllers["controller"].instanceID,
		"optional recovery never restarts the required controller")
	h.m.mu.Unlock()
	_, _, ok = h.m.Lookup("controller")
	require.True(t, ok, "optional failure never disturbs the required core")
	effect, err := os.ReadFile(filepath.Join(controllerStateDir(h), "effect.up"))
	require.NoError(t, err)
	require.Equal(t, "1", string(effect), "recovery adopts the owned effect generation")
	require.GreaterOrEqual(t, readActivateCount(controllerStateDir(h)), 1)
}

// F26-4: boot never downloads optional bytes: a desired package with
// no installed bytes stays absent with its intent untouched while
// core serves.
func TestF026NoOptionalDownloadAtBoot(t *testing.T) {
	root := shortTestRoot(t)
	installLifecyclePackage(t, root, "controller", controllerManifest, testFixtureKnobs{Role: "controller"})
	controllerEntry := testCatalogEntry("controller", "1.0.0", true, "")
	controllerEntry.Capabilities = []string{"native.extension.lifecycle"}
	require.NoError(t, packages.WriteReceipt(filepath.Join(root, "controller"), controllerEntry))
	// No sample bytes are installed: any acquisition would have to
	// download from the invalid catalog URL and fail loudly.
	sampleEntry := testCatalogEntry("sample", "1.0.0", false, "controller")
	sampleEntry.Capabilities = []string{"native.actor.read"}
	installation, err := packages.EnsureInstallation(root, "sample")
	require.NoError(t, err)
	_, err = packages.StoreIntent(root, "sample", installation, true, nil, "0")
	require.NoError(t, err)

	m := NewManager(root, "controller")
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	configureTestCatalog(t, m, root, controllerEntry, sampleEntry)
	require.NoError(t, m.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return testAuthorizeMux(m, instanceID)
	}))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })
	_, _, ok := m.Lookup("controller")
	require.True(t, ok, "required core is ready while the optional package is absent")

	// Past the 0, 1, and 3 second attempts: still no bytes, no
	// pending marker, no generation change, no host call for the
	// never-satisfied pin.
	time.Sleep(3500 * time.Millisecond)
	_, err = os.Lstat(filepath.Join(root, "sample"))
	require.True(t, os.IsNotExist(err), "boot must not download optional bytes")
	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation)
	require.True(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
	observed := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleFailed, observed.State)
}

// F26-5: the maintenance guard holds the exact root-owned 0755
// directory-inode contract with no lockfile, and the held probe
// validates the same metadata. Non-root fixture directories always
// refuse and read fenced; the positive path runs only with root
// ownership, which the native lane proves across namespaces.
func TestF026MaintenanceGuardRootOwnedContract(t *testing.T) {
	owned := t.TempDir()
	require.NoError(t, os.Chmod(owned, 0o755))
	if os.Geteuid() != 0 {
		_, err := acquireMaintenanceGuard(owned)
		require.Error(t, err, "non-root ownership must refuse the guard")
		require.True(t, maintenanceHeld(owned), "unsafe metadata reads fenced")
	} else {
		release, err := acquireMaintenanceGuard(owned)
		require.NoError(t, err)
		require.False(t, maintenanceHeld(owned))
		holder, err := os.OpenFile(owned, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		require.NoError(t, err)
		require.NoError(t, syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
		_, err = acquireMaintenanceGuard(owned)
		require.Error(t, err, "exclusive exclusion must refuse the shared guard")
		require.True(t, maintenanceHeld(owned))
		require.NoError(t, syscall.Flock(int(holder.Fd()), syscall.LOCK_UN))
		require.NoError(t, holder.Close())
		release()
		require.NoError(t, os.WriteFile(filepath.Join(owned, "operation.json"), []byte(`{}`), 0o600))
		_, err = acquireMaintenanceGuard(owned)
		require.Error(t, err, "the marker must refuse the shared guard")
		require.True(t, maintenanceHeld(owned))
	}

	loose := t.TempDir()
	require.NoError(t, os.Chmod(loose, 0o700))
	_, err := acquireMaintenanceGuard(loose)
	require.Error(t, err, "wrong mode must refuse the guard")
	require.True(t, maintenanceHeld(loose), "wrong mode reads fenced")

	absent := filepath.Join(t.TempDir(), "absent")
	_, err = acquireMaintenanceGuard(absent)
	require.Error(t, err, "absent directory must refuse the guard")
	require.True(t, maintenanceHeld(absent), "absent directory reads fenced")
	require.False(t, maintenanceHeld(""), "standalone Fountain has no fence")
}

// Snapshot coherence with invalid requested state: a malformed
// optional intent is preserved as a generation-zero row across
// snapshot reads, state serves it failed without host calls, and
// reconcile reports it failed without rejecting the pass.
func TestF026SnapshotCoherenceWithInvalidIntent(t *testing.T) {
	root := t.TempDir()
	m := controlManager(t, root)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".intents"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".intents", "sample.json"), []byte(`{"schema":`), 0o600))

	first, reject := m.ControlSnapshot(context.Background(), "soda")
	require.Nil(t, reject)
	second, reject := m.ControlSnapshot(context.Background(), "soda")
	require.Nil(t, reject)
	require.Equal(t, first, second, "snapshot reads are coherent")
	require.Len(t, first.Entries, 2)
	require.Equal(t, "sample", first.Entries[0].PackageID)
	require.Equal(t, "0", first.Entries[0].Generation)
	require.False(t, first.Entries[0].DesiredEnabled)

	state, reject := m.ControlState(context.Background(), "soda", "sample")
	require.Nil(t, reject)
	require.False(t, state.AdmissionOpen)
	require.Equal(t, "0", state.Entry.Generation)
	observed := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleFailed, observed.State)

	reconciled, reject := m.ControlReconcile(context.Background(), "soda")
	require.Nil(t, reject, "invalid intent fails its row, never the pass")
	byID := make(map[string]ControlReconcileEntry)
	for _, entry := range reconciled.Entries {
		byID[entry.PackageID] = entry
	}
	malformed := byID["sample"]
	require.Equal(t, "available", malformed.State)
	require.Nil(t, malformed.Error)
}
