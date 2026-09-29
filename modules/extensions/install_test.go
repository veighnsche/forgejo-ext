// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func installationPackage(t *testing.T, version string) string {
	t.Helper()
	source := t.TempDir()
	m := Manifest{Protocol: Protocol, ID: "example", Name: "Example", Version: version, Executable: "backend", Pages: []Page{{ID: "home", Title: "Home", Scope: "global", Entry: "main.js"}}}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(source, "extension.json"), b, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(source, "backend"), []byte("prebuilt executable fixture"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(source, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "assets/main.js"), []byte("export function mount() {}"), 0o644))
	return source
}

func TestInstallAndReplacePreserveActivationAndData(t *testing.T) {
	root := t.TempDir()
	m, err := Install(root, installationPackage(t, "1"), false)
	require.NoError(t, err)
	require.Equal(t, "example", m.ID)
	require.NoError(t, SetEnabled(root, m.ID, false))
	dataDir := filepath.Join(root, ".data", m.ID)
	require.NoError(t, os.MkdirAll(dataDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "state"), []byte("keep"), 0o600))
	_, err = Install(root, installationPackage(t, "2"), false)
	require.ErrorContains(t, err, "already installed")
	_, err = Install(root, installationPackage(t, "2"), true)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, m.ID, ".disabled"))
	data, err := os.ReadFile(filepath.Join(dataDir, "state"))
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
	m, err = LoadManifest(filepath.Join(root, m.ID))
	require.NoError(t, err)
	require.Equal(t, "2", m.Version)
	require.NoError(t, SetEnabled(root, m.ID, true))
	require.NoFileExists(t, filepath.Join(root, m.ID, ".disabled"))
}

func TestPackageMutationRequiresStoppedManager(t *testing.T) {
	root := t.TempDir()
	lock, err := AcquirePackageLock(root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	_, err = Install(root, installationPackage(t, "1"), false)
	require.ErrorContains(t, err, "stop Forgejo")
	require.ErrorContains(t, SetEnabled(root, "example", true), "stop Forgejo")
}

func TestInvalidPackageDoesNotReplaceInstalledPackage(t *testing.T) {
	root := t.TempDir()
	_, err := Install(root, installationPackage(t, "1"), false)
	require.NoError(t, err)
	source := installationPackage(t, "2")
	require.NoError(t, os.Remove(filepath.Join(source, "assets/main.js")))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(source, "assets/main.js")))
	_, err = Install(root, source, true)
	require.ErrorContains(t, err, "symlinks")
	m, err := LoadManifest(filepath.Join(root, "example"))
	require.NoError(t, err)
	require.Equal(t, "1", m.Version)
	require.Error(t, SetEnabled(root, "../example", true))
}
