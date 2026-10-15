// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	sdk "forgejo.org/extension-sdk"

	"github.com/stretchr/testify/require"
)

func catalogArchive(t *testing.T, mutate func(files map[string][]byte)) []byte {
	t.Helper()
	files := map[string][]byte{}
	manifest := sdk.Manifest{Protocol: sdk.Protocol, ID: "tailnet", Name: "Tailnet", Version: "1.2.3", Executable: "backend", Pages: []sdk.Page{{ID: "home", Title: "Home", Scope: "global", Entry: "main.js"}}}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	files["extension.json"] = encoded
	files["backend"] = []byte("prebuilt executable fixture")
	files["assets/main.js"] = []byte("export function mount() {}")
	if mutate != nil {
		mutate(files)
	}
	var archive bytes.Buffer
	unzip := gzip.NewWriter(&archive)
	writer := tar.NewWriter(unzip)
	for name, body := range files {
		mode := int64(0o644)
		if name == "backend" {
			mode = 0o755
		}
		require.NoError(t, writer.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, err := writer.Write(body)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	require.NoError(t, unzip.Close())
	return archive.Bytes()
}

func catalogServer(t *testing.T, body []byte, status int) (*httptest.Server, sdk.CatalogPackage) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	sum := sha256.Sum256(body)
	entry := sdk.CatalogPackage{
		ID: "tailnet", Version: "1.2.3", URL: server.URL + "/tailnet.tar.gz",
		Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:]),
		FountainProto: sdk.Protocol, BrowserContract: sdk.BrowserContractVersion,
	}
	return server, entry
}

func testClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	client := server.Client()
	client.Timeout = acquireTimeout
	return client
}

func TestAcquireVerifiesAndStages(t *testing.T) {
	body := catalogArchive(t, nil)
	server, entry := catalogServer(t, body, http.StatusOK)
	root := t.TempDir()
	stage, err := AcquireWithClient(context.Background(), root, entry, testClient(t, server))
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(stage) })
	manifest, err := sdk.LoadManifest(stage)
	require.NoError(t, err)
	require.Equal(t, "tailnet", manifest.ID)
	store, err := OpenPackageStore(root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	installed, err := store.InstallStaged(stage, InstallOptions{Entry: &entry})
	require.NoError(t, err)
	require.Equal(t, "1.2.3", installed.Version)
	require.NoError(t, ValidateInstalled(root, entry))
}

func TestAcquireRejectsDownloadFailure(t *testing.T) {
	body := catalogArchive(t, nil)
	server, entry := catalogServer(t, body, http.StatusServiceUnavailable)
	root := t.TempDir()
	_, err := AcquireWithClient(context.Background(), root, entry, testClient(t, server))
	require.ErrorContains(t, err, "download failed")
	entries, readErr := os.ReadDir(root)
	require.NoError(t, readErr)
	require.Empty(t, entries, "failed acquisition must leave no staging behind")
}

func TestAcquireRejectsChecksumMismatch(t *testing.T) {
	body := catalogArchive(t, nil)
	server, entry := catalogServer(t, body, http.StatusOK)
	entry.SHA256 = strings.Repeat("0", 64)
	_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.ErrorContains(t, err, "checksum mismatch")
}

func TestAcquireRejectsSizeMismatch(t *testing.T) {
	body := catalogArchive(t, nil)
	server, entry := catalogServer(t, body, http.StatusOK)
	entry.Size++
	_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.ErrorContains(t, err, "size mismatch")
}

func TestAcquireRejectsIdentityMismatch(t *testing.T) {
	body := catalogArchive(t, nil)
	server, entry := catalogServer(t, body, http.StatusOK)
	entry.Version = "9.9.9"
	_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.ErrorContains(t, err, "identity does not match")
}

func TestAcquireRejectsEscapingArchive(t *testing.T) {
	var archive bytes.Buffer
	unzip := gzip.NewWriter(&archive)
	writer := tar.NewWriter(unzip)
	payload := []byte("escape")
	require.NoError(t, writer.WriteHeader(&tar.Header{Name: "../../escape", Mode: 0o644, Size: int64(len(payload)), Typeflag: tar.TypeReg}))
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, unzip.Close())
	server, entry := catalogServer(t, archive.Bytes(), http.StatusOK)
	_, err = AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.ErrorContains(t, err, "escapes staging")
}

func TestAcquireRejectsIncompatibleCatalogEntry(t *testing.T) {
	entry := sdk.CatalogPackage{ID: "tailnet", Version: "1", URL: "https://releases.example.com/t.tar.gz", Size: 10, SHA256: strings.Repeat("a", 64), FountainProto: sdk.Protocol + 1, BrowserContract: sdk.BrowserContractVersion}
	_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, &http.Client{})
	require.ErrorContains(t, err, "incompatible")
}
