// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"os"
	"path/filepath"
	"testing"

	sdk "forgejo.org/extension-sdk"

	"github.com/stretchr/testify/require"
)

func installationPackage(t *testing.T, version string) string {
	t.Helper()
	source := t.TempDir()
	m := sdk.Manifest{Protocol: sdk.Protocol, ID: "example", Name: "Example", Version: version, Executable: "backend", Pages: []sdk.Page{{ID: "home", Title: "Home", Scope: "global", Entry: "main.js"}}}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(source, "extension.json"), b, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(source, "backend"), []byte("prebuilt executable fixture"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(source, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "assets/main.js"), []byte("export function mount() {}"), 0o644))
	return source
}

func openTestStore(t *testing.T, root string) *PackageStore {
	t.Helper()
	store, err := OpenPackageStore(root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestInstallAndReplacePreserveActivationAndData(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	m, err := store.InstallPrebuilt(installationPackage(t, "1"), false)
	require.NoError(t, err)
	require.Equal(t, "example", m.ID)
	disabled, err := store.SetDesired(m.ID, false)
	require.NoError(t, err)
	require.Equal(t, "1", disabled.Generation)
	require.False(t, disabled.DesiredEnabled)
	dataDir := filepath.Join(root, ".data", m.ID)
	require.NoError(t, os.MkdirAll(dataDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "state"), []byte("keep"), 0o600))
	_, err = store.InstallPrebuilt(installationPackage(t, "2"), false)
	require.ErrorContains(t, err, "already installed")
	_, err = store.InstallPrebuilt(installationPackage(t, "2"), true)
	require.NoError(t, err)
	// Replacement preserves the installation UUID and desired intent,
	// and leaves no fixed backup behind.
	preserved, err := LoadIntent(root, m.ID)
	require.NoError(t, err)
	require.Equal(t, "1", preserved.Generation)
	require.False(t, preserved.DesiredEnabled)
	require.NoDirExists(t, filepath.Join(root, ".previous-"+m.ID))
	data, err := os.ReadFile(filepath.Join(dataDir, "state"))
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
	m, err = sdk.LoadManifest(filepath.Join(root, "example"))
	require.NoError(t, err)
	require.Equal(t, "2", m.Version)
	enabled, err := store.SetDesired("example", true)
	require.NoError(t, err)
	require.Equal(t, "2", enabled.Generation)
	require.True(t, enabled.DesiredEnabled)
}

func TestPackageMutationRequiresStoppedManager(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	_, err := store.InstallPrebuilt(installationPackage(t, "1"), false)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	lock, err := AcquirePackageLock(root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	_, err = OpenPackageStore(root)
	require.ErrorContains(t, err, "stop Forgejo")
}

func TestRemoveRetainsPrivateData(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	_, err := store.InstallPrebuilt(installationPackage(t, "1"), false)
	require.NoError(t, err)
	dataDir := filepath.Join(root, ".data", "example")
	require.NoError(t, os.MkdirAll(dataDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "state"), []byte("keep"), 0o600))
	require.Error(t, store.Remove("../example"))
	require.DirExists(t, filepath.Join(root, "example"))
	require.NoError(t, store.Remove("example"))
	require.NoDirExists(t, filepath.Join(root, "example"))
	require.FileExists(t, filepath.Join(dataDir, "state"))
	_, err = store.InstallPrebuilt(installationPackage(t, "2"), false)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dataDir, "state"))
}

func TestRemoveRejectsSymlinkPackage(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "state"), []byte("keep"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "example")))
	require.ErrorContains(t, store.Remove("example"), "regular directory")
	require.FileExists(t, filepath.Join(outside, "state"))
}

func TestInvalidPackageDoesNotReplaceInstalledPackage(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	_, err := store.InstallPrebuilt(installationPackage(t, "1"), false)
	require.NoError(t, err)
	source := installationPackage(t, "2")
	require.NoError(t, os.Remove(filepath.Join(source, "assets/main.js")))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(source, "assets/main.js")))
	_, err = store.InstallPrebuilt(source, true)
	require.ErrorContains(t, err, "symlinks")
	m, err := sdk.LoadManifest(filepath.Join(root, "example"))
	require.NoError(t, err)
	require.Equal(t, "1", m.Version)
	_, err = store.SetDesired("../example", true)
	require.Error(t, err)
}
