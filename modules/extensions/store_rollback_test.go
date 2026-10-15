// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	sdk "forgejo.org/extension-sdk"

	"github.com/stretchr/testify/require"
)

func forceDirectorySyncFailure(t *testing.T) {
	t.Helper()
	previous := directorySync
	directorySync = func(string) error { return errors.New("injected root sync failure") }
	t.Cleanup(func() { directorySync = previous })
}

// A root fsync failure after both renames restores the old package:
// old bytes, receipt, installation UUID, and intent survive, and the
// replacement is not left installed.
func TestInstallSyncFailureRestoresPrevious(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	server1, entry1 := serveArchive(t, versionedArchive(t, "sample", "1.0.0", "", nil), "sample", "1.0.0")
	installAcquired(t, store, entry1, testClient(t, server1), false)
	uuid, err := LoadInstallation(root, "sample")
	require.NoError(t, err)
	desired, err := store.SetDesired("sample", true)
	require.NoError(t, err)

	server2, entry2 := serveArchive(t, versionedArchive(t, "sample", "2.0.0", "", nil), "sample", "2.0.0")
	stage, err := AcquireWithClient(context.Background(), root, entry2, testClient(t, server2))
	require.NoError(t, err)
	defer os.RemoveAll(stage)

	forceDirectorySyncFailure(t)
	_, err = store.InstallStaged(stage, InstallOptions{Replace: true, Entry: &entry2})
	require.ErrorContains(t, err, "previous version restored")

	manifest, err := sdk.LoadManifest(filepath.Join(root, "sample"))
	require.NoError(t, err)
	require.Equal(t, "1.0.0", manifest.Version)
	require.NoError(t, ValidateInstalled(root, entry1))
	preserved, err := LoadInstallation(root, "sample")
	require.NoError(t, err)
	require.Equal(t, uuid, preserved)
	intent, err := LoadIntent(root, "sample")
	require.NoError(t, err)
	require.Equal(t, desired, intent)
	require.NoFileExists(t, filepath.Join(root, ".previous-sample"))
}

// When the rollback itself cannot proceed, both packages stay intact and
// the error names exactly where the previous version is.
func TestRollbackSwapBlockedKeepsBothPackages(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	server1, entry1 := serveArchive(t, versionedArchive(t, "sample", "1.0.0", "", nil), "sample", "1.0.0")
	installAcquired(t, store, entry1, testClient(t, server1), false)

	// Crash-point state after both renames: new bytes at the target, old
	// bytes in the fixed backup.
	require.NoError(t, os.Rename(filepath.Join(root, "sample"), filepath.Join(root, ".previous-sample")))
	server2, entry2 := serveArchive(t, versionedArchive(t, "sample", "2.0.0", "", nil), "sample", "2.0.0")
	stage, err := AcquireWithClient(context.Background(), root, entry2, testClient(t, server2))
	require.NoError(t, err)
	defer os.RemoveAll(stage)
	require.NoError(t, os.Rename(stage, filepath.Join(root, "sample")))

	require.NoError(t, os.Chmod(root, 0o500))
	defer func() { require.NoError(t, os.Chmod(root, 0o700)) }()
	err = rollbackInstallSwap(root, "sample")
	require.ErrorContains(t, err, ".previous-sample")
	require.DirExists(t, filepath.Join(root, "sample"))
	require.DirExists(t, filepath.Join(root, ".previous-sample"))
}

// A missing backup fails the restore rename: the aside copy returns to
// the target and the error still names the previous version location.
func TestRollbackSwapRestoresBothOnRestoreFailure(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sample"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sample", "marker"), []byte("new"), 0o600))
	err := rollbackInstallSwap(root, "sample")
	require.ErrorContains(t, err, ".previous-sample")
	data, readErr := os.ReadFile(filepath.Join(root, "sample", "marker"))
	require.NoError(t, readErr)
	require.Equal(t, "new", string(data))
	entries, readErr := os.ReadDir(root)
	require.NoError(t, readErr)
	for _, entry := range entries {
		require.NotContains(t, entry.Name(), ".rollback-")
	}
}
