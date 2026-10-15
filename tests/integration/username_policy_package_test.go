// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/modules/extensions"
	"forgejo.org/modules/json"

	"github.com/stretchr/testify/require"
)

// Prepare an actual pinned archive through offline bootstrap, including the
// generated receipt. Policy tests exercise startup through the same required
// package gate as production, rather than trusting a loose executable directory.
func prepareUsernamePolicyPackage(t *testing.T, root, manifest, executable, mode string, available bool) string {
	t.Helper()
	script := "#!/bin/sh\nexport FORGEJO_EXTENSION_TEST_USERNAME_POLICY=" + mode + "\nexec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "'\n"
	var archive bytes.Buffer
	compressed := gzip.NewWriter(&archive)
	writer := tar.NewWriter(compressed)
	for _, file := range []struct {
		name string
		body string
		mode int64
	}{{"extension.json", manifest, 0o600}, {"run", script, 0o700}} {
		require.NoError(t, writer.WriteHeader(&tar.Header{Name: file.name, Mode: file.mode, Size: int64(len(file.body))}))
		_, err := writer.Write([]byte(file.body))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	require.NoError(t, compressed.Close())
	var declared sdk.Manifest
	require.NoError(t, json.Unmarshal([]byte(manifest), &declared))
	digest := sha256.Sum256(archive.Bytes())
	entry := sdk.CatalogPackage{
		ID: declared.ID, Version: declared.Version, URL: "https://127.0.0.1:1/policy.tar.gz",
		Size: int64(archive.Len()), SHA256: hex.EncodeToString(digest[:]), Required: true,
		FountainProto: sdk.Protocol, BrowserContract: sdk.BrowserContractVersion, Capabilities: declared.Capabilities,
	}
	catalog := sdk.Catalog{Version: sdk.CatalogVersion, Packages: []sdk.CatalogPackage{entry}}
	require.NoError(t, sdk.ValidateCatalog(catalog))
	data, err := json.Marshal(catalog)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(root, 0o700))
	catalogPath := filepath.Join(root, ".username-policy-catalog.json")
	require.NoError(t, os.WriteFile(catalogPath, data, 0o600))
	if available {
		cache := filepath.Join(root, ".cache", "packages")
		require.NoError(t, os.MkdirAll(cache, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(cache, entry.SHA256+".tar.gz"), archive.Bytes(), 0o600))
		store, err := extensions.OpenPackageStore(root)
		require.NoError(t, err)
		defer func() { require.NoError(t, store.Close()) }()
		require.NoError(t, extensions.BootstrapRequired(t.Context(), store, catalog, []string{"policy"}, nil))
		require.NoError(t, extensions.ValidateInstalled(root, entry))
	}
	return catalogPath
}
