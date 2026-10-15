// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	sdk "forgejo.org/extension-sdk"

	"github.com/stretchr/testify/require"
)

// versionedArchive builds one valid package archive for an arbitrary id,
// version and lifecycle controller.
func versionedArchive(t *testing.T, id, version, controller string, mutate func(files map[string][]byte)) []byte {
	t.Helper()
	manifest := sdk.Manifest{Protocol: sdk.Protocol, ID: id, Name: id, Version: version, Executable: "backend", LifecycleController: controller, Pages: []sdk.Page{{ID: "home", Title: "Home", Scope: "global", Entry: "main.js"}}}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	files := map[string][]byte{
		"extension.json": encoded,
		"backend":        []byte("prebuilt executable fixture"),
		"assets/main.js": []byte("export function mount() {}"),
	}
	if mutate != nil {
		mutate(files)
	}
	return tarGzip(t, func(writer *tar.Writer) {
		writeFixtureTar(t, writer, files)
	})
}

// serveArchive serves one archive body as a catalog entry.
func serveArchive(t *testing.T, body []byte, id, version string) (*httptest.Server, sdk.CatalogPackage) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	sum := sha256.Sum256(body)
	return server, sdk.CatalogPackage{
		ID: id, Version: version, URL: server.URL + "/" + id + ".tar.gz",
		Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:]),
		FountainProto: sdk.Protocol, BrowserContract: sdk.BrowserContractVersion,
	}
}

// installAcquired runs the full verified flow: download, stage, swap.
func installAcquired(t *testing.T, store *PackageStore, entry sdk.CatalogPackage, client *http.Client, replace bool) {
	t.Helper()
	stage, err := AcquireWithClient(context.Background(), store.Root(), entry, client)
	require.NoError(t, err)
	defer os.RemoveAll(stage)
	_, err = store.InstallStaged(stage, InstallOptions{Replace: replace, Entry: &entry})
	require.NoError(t, err)
}

func copyPackageDir(t *testing.T, source, target string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(target, 0o700))
	require.NoError(t, filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		relative, err := filepath.Rel(source, name)
		require.NoError(t, err)
		if relative == "." {
			return nil
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(name)
		require.NoError(t, err)
		info, err := entry.Info()
		require.NoError(t, err)
		return os.WriteFile(destination, data, info.Mode().Perm())
	}))
}

func TestRecoverPackageRestoresBackup(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	server, entry := serveArchive(t, versionedArchive(t, "sample", "1.0.0", "", nil), "sample", "1.0.0")
	installAcquired(t, store, entry, testClient(t, server), false)

	// Crash between the two renames: old bytes sit in the fixed backup
	// while the target is absent.
	target := filepath.Join(root, "sample")
	backup := filepath.Join(root, ".previous-sample")
	require.NoError(t, os.Rename(target, backup))
	require.NoError(t, RecoverPackage(root, "sample"))
	require.DirExists(t, target)
	require.NoFileExists(t, backup)
	require.NoError(t, ValidateInstalled(root, entry))
}

func TestRecoverPackageRemovesBackupAfterValidTarget(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	server, entry := serveArchive(t, versionedArchive(t, "sample", "1.0.0", "", nil), "sample", "1.0.0")
	installAcquired(t, store, entry, testClient(t, server), false)

	// Crash after the swap before backup cleanup: both exist and the
	// target validates, so exactly that backup is removed.
	copyPackageDir(t, filepath.Join(root, "sample"), filepath.Join(root, ".previous-sample"))
	require.NoError(t, RecoverPackage(root, "sample"))
	require.NoError(t, ValidateInstalled(root, entry))
	require.NoFileExists(t, filepath.Join(root, ".previous-sample"))
}

func TestRecoverPackageKeepsBackupForInvalidTarget(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	server, entry := serveArchive(t, versionedArchive(t, "sample", "1.0.0", "", nil), "sample", "1.0.0")
	installAcquired(t, store, entry, testClient(t, server), false)
	copyPackageDir(t, filepath.Join(root, "sample"), filepath.Join(root, ".previous-sample"))

	// An invalid target is unavailable and does not execute; the backup
	// is kept for inspection rather than pruned.
	require.NoError(t, os.Remove(filepath.Join(root, "sample", receiptFileName)))
	require.ErrorContains(t, RecoverPackage(root, "sample"), "unavailable")
	require.DirExists(t, filepath.Join(root, ".previous-sample"))
	require.DirExists(t, filepath.Join(root, "sample"))

	require.NoError(t, RecoverPackage(root, "absent"))
}

func TestFailedValidationLeavesInstalledIntact(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	server, entry := serveArchive(t, versionedArchive(t, "sample", "1.0.0", "", nil), "sample", "1.0.0")
	installAcquired(t, store, entry, testClient(t, server), false)
	before, err := LoadInstallation(root, "sample")
	require.NoError(t, err)

	// A stage that fails validation never reaches the renames: old bytes,
	// receipt, UUID and intent space stay exactly as they were.
	broken := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(broken, "extension.json"), []byte(`{"protocol":1,"id":"sample","name":"Sample","version":"2.0.0","executable":"backend"}`), 0o600))
	_, err = store.InstallStaged(broken, InstallOptions{Replace: true, Entry: &entry})
	require.Error(t, err)
	manifest, err := sdk.LoadManifest(filepath.Join(root, "sample"))
	require.NoError(t, err)
	require.Equal(t, "1.0.0", manifest.Version)
	require.NoError(t, ValidateInstalled(root, entry))
	after, err := LoadInstallation(root, "sample")
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoFileExists(t, filepath.Join(root, ".previous-sample"))
}

func TestReplaceUpdatesReceiptPreservingIdentity(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	server1, entry1 := serveArchive(t, versionedArchive(t, "sample", "1.0.0", "", nil), "sample", "1.0.0")
	installAcquired(t, store, entry1, testClient(t, server1), false)
	uuid, err := LoadInstallation(root, "sample")
	require.NoError(t, err)
	desired, err := store.SetDesired("sample", true)
	require.NoError(t, err)

	server2, entry2 := serveArchive(t, versionedArchive(t, "sample", "2.0.0", "", nil), "sample", "2.0.0")
	installAcquired(t, store, entry2, testClient(t, server2), true)
	require.NoError(t, ValidateInstalled(root, entry2))
	preserved, err := LoadInstallation(root, "sample")
	require.NoError(t, err)
	require.Equal(t, uuid, preserved)
	intent, err := LoadIntent(root, "sample")
	require.NoError(t, err)
	require.Equal(t, desired, intent)
}

func TestPrebuiltRejectsReservedState(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	for _, reserved := range []string{receiptFileName, ".disabled"} {
		source := installationPackage(t, "1")
		require.NoError(t, os.WriteFile(filepath.Join(source, reserved), []byte("{}"), 0o600))
		_, err := store.InstallPrebuilt(source, false)
		require.ErrorContains(t, err, "reserved")
	}
	// Prebuilt installs carry no verified receipt: only the catalog flow
	// binds installed bytes to an archive digest.
	_, err := store.InstallPrebuilt(installationPackage(t, "1"), false)
	require.NoError(t, err)
	_, err = LoadReceipt(filepath.Join(root, "example"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
