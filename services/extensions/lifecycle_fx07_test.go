// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"
	"forgejo.org/modules/json"

	"github.com/stretchr/testify/require"
)

// writeClaimFixture installs one package directory whose manifest
// declares a lifecycle controller, without any executable backend:
// every FX07 refusal lands before any process starts, so no helper
// process is needed.
func writeClaimFixture(t *testing.T, root, id, version, controller string) {
	t.Helper()
	packageDir := filepath.Join(root, id)
	require.NoError(t, os.MkdirAll(packageDir, 0o700))
	manifest := map[string]any{
		"protocol": 1, "id": id, "name": "Test", "version": version,
		"executable": "extension", "capabilities": []string{"native.actor.read"},
	}
	if controller != "" {
		manifest["lifecycle_controller"] = controller
	}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), encoded, 0o600))
}

// danglingManager returns a started manager whose configured catalog
// is exactly entries: an omitted target is a dangling reference, not
// a standalone package.
func danglingManager(root string, entries ...sdk.CatalogPackage) *Manager {
	m := NewManager(root)
	m.catalog = append([]sdk.CatalogPackage(nil), entries...)
	m.catalogSHA = "test-sha"
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	return m
}

// seedIntent persists one desired state and returns its generation
// for the request compare-and-swap base.
func seedIntent(t *testing.T, root, id string, desired bool) string {
	t.Helper()
	installation, err := packages.EnsureInstallation(root, id)
	require.NoError(t, err)
	intent, err := packages.StoreIntent(root, id, installation, desired, nil, "0")
	require.NoError(t, err)
	return intent.Generation
}

func loadRawIntent(t *testing.T, root, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".intents", id+".json"))
	require.NoError(t, err)
	return string(data)
}

// FX07-1: enable with a dangling controller claim (configured catalog
// omits the target, installed manifest declares a controller) fails
// with the precise catalog error before any reservation, identity,
// persisted intent, backend, or controller effect.
func TestFX07DanglingEnableRefusesBeforeEffects(t *testing.T) {
	root := shortTestRoot(t)
	writeClaimFixture(t, root, "sample", "1.0", "controller")
	generation := seedIntent(t, root, "sample", false)
	before := loadRawIntent(t, root, "sample")
	m := danglingManager(root, testCatalogEntry("other", "1.0", false, ""))
	defer func() { _ = m.Close() }()

	result := m.requestEnabled(context.Background(), "sample", "", generation, "test-sha", true)

	require.NoError(t, packages.ValidateLifecycleResult(result))
	require.Equal(t, packages.LifecycleFailed, result.State)
	require.NotNil(t, result.Error)
	require.Equal(t, packages.LifecycleErrCatalogBinding, result.Error.Code)
	require.Contains(t, result.Error.Message, "controller")
	require.Contains(t, result.Error.Message, "catalog")
	require.Equal(t, before, loadRawIntent(t, root, "sample"), "refusal must persist no intent change")
	m.mu.Lock()
	require.Empty(t, m.running)
	require.Empty(t, m.controllers)
	require.Empty(t, m.busy)
	require.Empty(t, m.mutation)
	m.mu.Unlock()
}

// FX07-2: disable with a dangling controller claim fails the same
// way: no backend-only stop may manufacture a successful disable
// with live external effects.
func TestFX07DanglingDisableRefusesBeforeEffects(t *testing.T) {
	root := shortTestRoot(t)
	writeClaimFixture(t, root, "sample", "1.0", "controller")
	generation := seedIntent(t, root, "sample", true)
	before := loadRawIntent(t, root, "sample")
	m := danglingManager(root, testCatalogEntry("other", "1.0", false, ""))
	defer func() { _ = m.Close() }()

	result := m.requestEnabled(context.Background(), "sample", "", generation, "test-sha", false)

	require.NoError(t, packages.ValidateLifecycleResult(result))
	require.Equal(t, packages.LifecycleFailed, result.State)
	require.NotNil(t, result.Error)
	require.Equal(t, packages.LifecycleErrCatalogBinding, result.Error.Code)
	require.Equal(t, before, loadRawIntent(t, root, "sample"), "refusal must persist no intent change")
	m.mu.Lock()
	require.Empty(t, m.running)
	require.Empty(t, m.mutation)
	m.mu.Unlock()
}

// FX07-3: the managed async admin path refuses dangling references
// at adoption, with no accepted operation and no task.
func TestFX07ManagedPathRefusesDanglingAtAdoption(t *testing.T) {
	root := shortTestRoot(t)
	writeClaimFixture(t, root, "sample", "1.0", "controller")
	generation := seedIntent(t, root, "sample", true)
	m := danglingManager(root, testCatalogEntry("other", "1.0", false, ""))
	defer func() { _ = m.Close() }()

	result := m.RequestEnabled(context.Background(), "sample", generation, "test-sha", false)

	require.NoError(t, packages.ValidateLifecycleResult(result))
	require.Equal(t, packages.LifecycleFailed, result.State)
	require.NotNil(t, result.Error)
	require.Equal(t, packages.LifecycleErrCatalogBinding, result.Error.Code)
	m.mu.Lock()
	require.Nil(t, m.activeOp, "refused adoption must start no mutation task")
	m.mu.Unlock()
}

// FX07-4: with no configured catalog at all, standalone backend-only
// behavior is unchanged, even when the manifest claims a controller.
func TestFX07StandaloneWithoutCatalogStillPermits(t *testing.T) {
	root := shortTestRoot(t)
	writeClaimFixture(t, root, "sample", "1.0", "controller")
	generation := seedIntent(t, root, "sample", false)
	m := danglingManager(root)
	defer func() { _ = m.Close() }()

	result := m.requestEnabled(context.Background(), "sample", "", generation, "test-sha", false)

	require.NoError(t, packages.ValidateLifecycleResult(result))
	require.Equal(t, packages.LifecycleDisabled, result.State, "standalone disable with no live backend still settles")
}

// FX07-5: the binding predicate resolves owned-by labels to catalog
// IDs: only a configured catalog that omits the target dangles.
func TestFX07BindingPredicateMatrix(t *testing.T) {
	root := shortTestRoot(t)
	configured := danglingManager(root, testCatalogEntry("other", "1.0", false, ""))
	defer func() { _ = configured.Close() }()
	standalone := danglingManager(root)
	defer func() { _ = standalone.Close() }()

	require.NoError(t, configured.checkControllerBinding("sample", ""), "no declared controller never dangles")
	require.NoError(t, standalone.checkControllerBinding("sample", "controller"), "no configured catalog never dangles")
	require.NoError(t, configured.checkControllerBinding("other", "controller"), "catalog-listed target resolves")

	err := configured.checkControllerBinding("sample", "controller")
	require.Error(t, err)
	var dangling *danglingBindingError
	require.ErrorAs(t, err, &dangling)
	require.Contains(t, err.Error(), "sample")
	require.Contains(t, err.Error(), "controller")
}

// FX07-6: registration mismatch (installed manifest claims a
// different controller than the catalog entry) never enables: the
// trusted catalog wins and the attempt fails closed with pending
// retained and no backend started.
func TestFX07RegistrationMismatchRefusesEnable(t *testing.T) {
	root := shortTestRoot(t)
	entry := testCatalogEntry("sample", "1.0", false, "controller-a")
	installCatalogPackage(t, root, entry, nil)
	writeClaimFixture(t, root, "sample", "1.0", "controller-b")
	require.NoError(t, packages.WriteReceipt(filepath.Join(root, "sample"), entry))
	generation := seedIntent(t, root, "sample", false)
	m := danglingManager(root, entry)
	defer func() { _ = m.Close() }()

	result := m.requestEnabled(context.Background(), "sample", "", generation, "test-sha", true)

	require.NoError(t, packages.ValidateLifecycleResult(result))
	require.NotEqual(t, packages.LifecycleEnabled, result.State, "mismatched registration must not enable")
	require.NotNil(t, result.Error)
	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.NotNil(t, intent.PendingOperation, "failed attempt retains pending for explicit retry")
	m.mu.Lock()
	require.Empty(t, m.running, "no backend may start on a mismatched registration")
	m.mu.Unlock()
}

// FX07-7: a missing required controller fails closed with no
// manufactured success: the relationship stays trusted, the
// controller simply is not there.
func TestFX07MissingControllerFailsClosed(t *testing.T) {
	root := shortTestRoot(t)
	entry := testCatalogEntry("sample", "1.0", false, "controller")
	installCatalogPackage(t, root, entry, nil)
	generation := seedIntent(t, root, "sample", false)
	m := danglingManager(root, entry)
	defer func() { _ = m.Close() }()

	result := m.requestEnabled(context.Background(), "sample", "", generation, "test-sha", true)

	require.NoError(t, packages.ValidateLifecycleResult(result))
	require.NotEqual(t, packages.LifecycleEnabled, result.State)
	require.NotNil(t, result.Error)
	require.NotEqual(t, packages.LifecycleErrCatalogBinding, result.Error.Code, "present binding must not report a binding error")
	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.NotNil(t, intent.PendingOperation, "unready controller retains pending for explicit retry")
}

// FX07-8: boot start of a dangling row refuses before any process
// starts and records the precise configuration failure.
func TestFX07BootStartDanglingRefuses(t *testing.T) {
	root := shortTestRoot(t)
	writeClaimFixture(t, root, "sample", "1.0", "controller")
	_ = seedIntent(t, root, "sample", true)
	m := danglingManager(root, testCatalogEntry("other", "1.0", false, ""))
	defer func() { _ = m.Close() }()

	intent, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.False(t, m.reconcileUncontrolledRow(context.Background(), sdk.CatalogPackage{ID: "sample"}, intent))

	m.mu.Lock()
	require.Empty(t, m.running, "dangling boot row must start no backend")
	m.mu.Unlock()
	failure := m.lastFailure("sample")
	require.NotNil(t, failure)
	require.Contains(t, failure.Message, "without a catalog entry")
}

// FX07-9: bindings survive restart from the catalog, never from
// memory: a restarted manager still refuses the dangling reference,
// and restoring the catalog entry moves the failure to the (absent)
// controller instead of the binding.
func TestFX07BindingsSurviveRestart(t *testing.T) {
	root := shortTestRoot(t)
	writeClaimFixture(t, root, "sample", "1.0", "controller")
	generation := seedIntent(t, root, "sample", false)

	omitting := []sdk.CatalogPackage{testCatalogEntry("other", "1.0", false, "")}
	first := danglingManager(root, omitting...)
	result := first.requestEnabled(context.Background(), "sample", "", generation, "test-sha", true)
	require.NotNil(t, result.Error)
	require.Equal(t, packages.LifecycleErrCatalogBinding, result.Error.Code)
	require.NoError(t, first.Close())

	restarted := danglingManager(root, omitting...)
	result = restarted.requestEnabled(context.Background(), "sample", "", generation, "test-sha", true)
	require.NotNil(t, result.Error)
	require.Equal(t, packages.LifecycleErrCatalogBinding, result.Error.Code, "restart must not heal a dangling binding")
	require.NoError(t, restarted.Close())

	restored := danglingManager(root, testCatalogEntry("sample", "1.0", false, "controller"))
	result = restored.requestEnabled(context.Background(), "sample", "", generation, "test-sha", true)
	require.NoError(t, packages.ValidateLifecycleResult(result))
	require.NotNil(t, result.Error)
	require.NotEqual(t, packages.LifecycleErrCatalogBinding, result.Error.Code, "restored binding resolves; only the absent controller blocks")
	require.NoError(t, restored.Close())
}
