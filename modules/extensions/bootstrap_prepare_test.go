// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "forgejo.org/extension-sdk"

	"github.com/stretchr/testify/require"
)

// multiArchiveServer serves several archive bodies by package id.
func multiArchiveServer(t *testing.T, bodies map[string][]byte) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".tar.gz")
		body, ok := bodies[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func catalogEntryFor(serverURL string, body []byte, id, version string, required bool, controller string) sdk.CatalogPackage {
	sum := sha256.Sum256(body)
	return sdk.CatalogPackage{
		ID: id, Version: version, URL: serverURL + "/" + id + ".tar.gz",
		Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:]), Required: required,
		FountainProto: sdk.Protocol, BrowserContract: sdk.BrowserContractVersion,
		LifecycleController: controller,
	}
}

func bootstrapFixture(t *testing.T) (*httptest.Server, sdk.Catalog) {
	t.Helper()
	bodies := map[string][]byte{
		"soda": versionedArchive(t, "soda", "1.0.0", "", nil),
	}
	server := multiArchiveServer(t, bodies)
	catalog := sdk.Catalog{Version: sdk.CatalogVersion, Packages: []sdk.CatalogPackage{
		catalogEntryFor(server.URL, bodies["soda"], "soda", "1.0.0", true, ""),
	}}
	require.NoError(t, sdk.ValidateCatalog(catalog))
	return server, catalog
}

func TestBootstrapRequiredDownloadsAndInstalls(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	server, catalog := bootstrapFixture(t)
	require.NoError(t, BootstrapRequired(context.Background(), store, catalog, []string{"soda"}, testClient(t, server)))
	require.NoError(t, ValidateInstalled(root, catalog.Packages[0]))
	installation, err := LoadInstallation(root, "soda")
	require.NoError(t, err)
	require.NotEmpty(t, installation)
	// Bootstrap writes no requested state: required packages are desired
	// by configuration, not by intent.
	absent, err := LoadIntent(root, "soda")
	require.NoError(t, err)
	require.Equal(t, "0", absent.Generation)
}

func TestBootstrapRequiredReusesInstalledAndCache(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	server, catalog := bootstrapFixture(t)
	require.NoError(t, BootstrapRequired(context.Background(), store, catalog, []string{"soda"}, testClient(t, server)))

	// Installed receipts with validated packages reboot without the
	// network: a dead server cannot fail the second pass.
	server.Close()
	dead := testClient(t, server)
	require.NoError(t, BootstrapRequired(context.Background(), store, catalog, []string{"soda"}, dead))

	// A fresh root installs from the verified cache while offline.
	offline := t.TempDir()
	offlineStore := openTestStore(t, offline)
	entry := catalog.Packages[0]
	cached, err := os.ReadFile(cachePath(root, entry.SHA256))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(offline, ".cache", "packages"), 0o700))
	require.NoError(t, os.WriteFile(cachePath(offline, entry.SHA256), cached, 0o600))
	require.NoError(t, BootstrapRequired(context.Background(), offlineStore, catalog, []string{"soda"}, dead))
	require.NoError(t, ValidateInstalled(offline, entry))
}

func TestBootstrapRequiredRejectsSelectionMismatch(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	server, catalog := bootstrapFixture(t)
	client := testClient(t, server)
	require.ErrorContains(t, BootstrapRequired(context.Background(), store, catalog, []string{"soda", "ghost"}, client), "required set")
	require.ErrorContains(t, BootstrapRequired(context.Background(), store, catalog, nil, client), "required set")
}

func prepareFixture(t *testing.T, root string) (sdk.Catalog, Snapshot, map[string]sdk.CatalogPackage) {
	t.Helper()
	v1 := map[string][]byte{
		"soda":    versionedArchive(t, "soda", "1.0.0", "", nil),
		"tailnet": versionedArchive(t, "tailnet", "1.0.0", "soda", nil),
		"other":   versionedArchive(t, "other", "1.0.0", "soda", nil),
	}
	server := multiArchiveServer(t, v1)
	client := testClient(t, server)
	store, err := OpenPackageStore(root)
	require.NoError(t, err)
	for _, id := range []string{"soda", "tailnet", "other"} {
		required := id == "soda"
		controller := "soda"
		if required {
			controller = ""
		}
		installAcquired(t, store, catalogEntryFor(server.URL, v1[id], id, "1.0.0", required, controller), client, false)
	}
	tailnetUUID, err := LoadInstallation(root, "tailnet")
	require.NoError(t, err)
	_, err = store.SetDesired("tailnet", true)
	require.NoError(t, err)
	otherUUID, err := LoadInstallation(root, "other")
	require.NoError(t, err)
	_, err = store.SetDesired("other", false)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	v2 := map[string][]byte{
		"soda":    versionedArchive(t, "soda", "2.0.0", "", nil),
		"tailnet": versionedArchive(t, "tailnet", "2.0.0", "soda", nil),
		"other":   versionedArchive(t, "other", "2.0.0", "soda", nil),
	}
	targetServer := multiArchiveServer(t, v2)
	target := sdk.Catalog{Version: sdk.CatalogVersion, Packages: []sdk.CatalogPackage{
		catalogEntryFor(targetServer.URL, v2["soda"], "soda", "2.0.0", true, ""),
		catalogEntryFor(targetServer.URL, v2["tailnet"], "tailnet", "2.0.0", false, "soda"),
		catalogEntryFor(targetServer.URL, v2["other"], "other", "2.0.0", false, "soda"),
	}}
	require.NoError(t, sdk.ValidateCatalog(target))
	require.NoError(t, ValidateCatalogSelection(target, []string{"soda"}))
	for _, entry := range target.Packages {
		_, err := downloadToCache(context.Background(), root, entry, testClient(t, targetServer))
		require.NoError(t, err)
	}
	byID := make(map[string]sdk.CatalogPackage)
	for _, entry := range target.Packages {
		byID[entry.ID] = entry
	}
	version := "1.0.0"
	snapshot := Snapshot{CatalogSHA256: strings.Repeat("c", 64), Entries: []SnapshotEntry{
		{PackageID: "soda", Generation: "0", DesiredEnabled: true, Required: true},
		{PackageID: "tailnet", InstallationID: &tailnetUUID, Generation: "1", DesiredEnabled: true, InstalledVersion: &version, Required: false},
		{PackageID: "other", InstallationID: &otherUUID, Generation: "1", DesiredEnabled: false, InstalledVersion: &version, Required: false},
	}}
	snapshot.Entries[1].PackageSHA256 = entrySHA(t, root, "tailnet")
	snapshot.Entries[2].PackageSHA256 = entrySHA(t, root, "other")
	return target, snapshot, byID
}

func entrySHA(t *testing.T, root, id string) *string {
	t.Helper()
	receipt, err := LoadReceipt(filepath.Join(root, id))
	require.NoError(t, err)
	return &receipt.ArchiveSHA256
}

func TestPrepareSelectsRequiredAndEnabledOnly(t *testing.T) {
	root := t.TempDir()
	target, snapshot, _ := prepareFixture(t, root)
	beforeTailnet, err := os.ReadFile(IntentPath(root, "tailnet"))
	require.NoError(t, err)
	beforeOther, err := os.ReadFile(IntentPath(root, "other"))
	require.NoError(t, err)

	store := openTestStore(t, root)
	prepared, err := store.PrepareRelease(target, snapshot, []string{"soda"})
	require.NoError(t, err)
	require.Len(t, prepared, 2)
	require.Equal(t, "soda", prepared[0].ID)
	require.Equal(t, "tailnet", prepared[1].ID)

	// Selected packages move to the target revision with receipts while
	// UUIDs and intents are preserved byte for byte.
	require.NoError(t, ValidateInstalled(root, target.Packages[0]))
	require.NoError(t, ValidateInstalled(root, target.Packages[1]))
	afterTailnet, err := os.ReadFile(IntentPath(root, "tailnet"))
	require.NoError(t, err)
	require.Equal(t, beforeTailnet, afterTailnet)
	afterOther, err := os.ReadFile(IntentPath(root, "other"))
	require.NoError(t, err)
	require.Equal(t, beforeOther, afterOther)

	// Disabled optionals are neither downloaded nor upgraded even with
	// verified target bytes cached.
	manifest, err := sdk.LoadManifest(filepath.Join(root, "other"))
	require.NoError(t, err)
	require.Equal(t, "1.0.0", manifest.Version)
}

func TestPrepareRefusesMissingCache(t *testing.T) {
	root := t.TempDir()
	target, snapshot, _ := prepareFixture(t, root)
	require.NoError(t, EvictCache(root, target.Packages[1].SHA256))

	store := openTestStore(t, root)
	_, err := store.PrepareRelease(target, snapshot, []string{"soda"})
	require.ErrorContains(t, err, "not cached")
	// The refusal lands before the first swap: nothing installed, no
	// intent touched.
	manifest, err := sdk.LoadManifest(filepath.Join(root, "soda"))
	require.NoError(t, err)
	require.Equal(t, "1.0.0", manifest.Version)
	intent, err := LoadIntent(root, "tailnet")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation)
}

func TestPrepareRejectsStaleSnapshot(t *testing.T) {
	root := t.TempDir()
	target, snapshot, _ := prepareFixture(t, root)
	store := openTestStore(t, root)

	// A concurrent admin change between freeze and prepare is reported,
	// never overwritten.
	installation, err := LoadInstallation(root, "tailnet")
	require.NoError(t, err)
	_, err = StoreIntent(root, "tailnet", installation, true, nil, "1")
	require.NoError(t, err)
	_, err = store.PrepareRelease(target, snapshot, []string{"soda"})
	require.ErrorContains(t, err, "does not match current state")
}

func TestPrepareRejectsEnabledAbsentFromTarget(t *testing.T) {
	root := t.TempDir()
	target, snapshot, _ := prepareFixture(t, root)
	ghost := ensureTestInstallation(t, root, "ghost")
	_, err := StoreIntent(root, "ghost", ghost, true, nil, "0")
	require.NoError(t, err)
	snapshot.Entries = append(snapshot.Entries, SnapshotEntry{PackageID: "ghost", InstallationID: &ghost, Generation: "1", DesiredEnabled: true})
	store := openTestStore(t, root)
	_, err = store.PrepareRelease(target, snapshot, []string{"soda"})
	require.ErrorContains(t, err, "absent from the target catalog")
}

func TestDecodeSnapshotRoundTrip(t *testing.T) {
	version, sha := "1.0.0", strings.Repeat("d", 64)
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	snapshot := Snapshot{CatalogSHA256: strings.Repeat("c", 64), Entries: []SnapshotEntry{
		{PackageID: "soda", Generation: "0", DesiredEnabled: true, Required: true},
		{PackageID: "tailnet", InstallationID: &uuid, Generation: "7", DesiredEnabled: true, InstalledVersion: &version, PackageSHA256: &sha},
	}}
	data, err := json.Marshal(snapshot)
	require.NoError(t, err)
	decoded, err := DecodeSnapshot(data)
	require.NoError(t, err)
	require.Equal(t, snapshot, decoded)

	_, err = DecodeSnapshot(append(data, byte('}')))
	require.Error(t, err)
	_, err = DecodeSnapshot([]byte(`{"catalog_sha256":"` + strings.Repeat("c", 64) + `","entries":[],"extra":1}`))
	require.Error(t, err)
	_, err = DecodeSnapshot(nil)
	require.Error(t, err)
}

func TestLoadCatalogFile(t *testing.T) {
	root := t.TempDir()
	document := `{"version":1,"packages":[{"id":"soda","version":"1.0.0","url":"https://releases.example.invalid/soda.tar.gz","size":1024,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","required":true,"fountain_protocol":1,"browser_contract":1,"capabilities":[]}]}`
	path := filepath.Join(root, "catalog.json")
	require.NoError(t, os.WriteFile(path, []byte(document), 0o600))

	catalog, sha, err := LoadCatalogFile(path)
	require.NoError(t, err)
	require.Len(t, catalog.Packages, 1)
	sum := sha256.Sum256([]byte(document))
	require.Equal(t, hex.EncodeToString(sum[:]), sha)

	// Empty means no available catalog, not an error.
	empty, _, err := LoadCatalogFile("")
	require.NoError(t, err)
	require.Empty(t, empty.Packages)

	_, _, err = LoadCatalogFile(filepath.Join(root, "missing.json"))
	require.ErrorContains(t, err, "unavailable")
	_, _, err = LoadCatalogFile("relative.json")
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "bad.json"), []byte("{invalid"), 0o600))
	_, _, err = LoadCatalogFile(filepath.Join(root, "bad.json"))
	require.ErrorContains(t, err, "unavailable")

	// Symlinks and group-writable files are refused.
	require.NoError(t, os.Symlink(path, filepath.Join(root, "link.json")))
	_, _, err = LoadCatalogFile(filepath.Join(root, "link.json"))
	require.ErrorContains(t, err, "unavailable")
	require.NoError(t, os.Chmod(path, 0o660))
	_, _, err = LoadCatalogFile(path)
	require.ErrorContains(t, err, "unavailable")
}

func TestValidateCatalogSelection(t *testing.T) {
	soda := sdk.CatalogPackage{ID: "soda", Version: "1.0.0", Required: true}
	tailnet := sdk.CatalogPackage{ID: "tailnet", Version: "1.0.0", LifecycleController: "soda"}
	catalog := sdk.Catalog{Version: 1, Packages: []sdk.CatalogPackage{soda, tailnet}}
	require.NoError(t, ValidateCatalogSelection(catalog, []string{"soda"}))
	require.ErrorContains(t, ValidateCatalogSelection(catalog, nil), "required set")
	require.ErrorContains(t, ValidateCatalogSelection(catalog, []string{"soda", "ghost"}), "required set")
	require.ErrorContains(t, ValidateCatalogSelection(catalog, []string{"soda", "soda"}), "invalid required")

	other := sdk.CatalogPackage{ID: "other", Version: "1.0.0", LifecycleController: "ghost"}
	require.ErrorContains(t, ValidateCatalogSelection(sdk.Catalog{Version: 1, Packages: []sdk.CatalogPackage{soda, other}}, []string{"soda"}), "unknown controller")

	mid := sdk.CatalogPackage{ID: "mid", Version: "1.0.0", Required: true, LifecycleController: "soda"}
	leaf := sdk.CatalogPackage{ID: "leaf", Version: "1.0.0", LifecycleController: "mid"}
	require.ErrorContains(t, ValidateCatalogSelection(sdk.Catalog{Version: 1, Packages: []sdk.CatalogPackage{soda, mid, leaf}}, []string{"soda", "mid"}), "another controlled package")
}
