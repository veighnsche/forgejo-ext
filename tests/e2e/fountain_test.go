// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	extension "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	web_extensions "forgejo.org/routers/web/extensions"
	extensions_service "forgejo.org/services/extensions"

	"github.com/stretchr/testify/require"
)

// Run the checked-in example through the real installer, backend manager and
// native callback service. Missing build inputs fail instead of skipping CI.
func startFountainExample(t *testing.T) {
	t.Helper()
	root, err := os.MkdirTemp("", "fe-") //nolint:usetesting // Native Unix socket paths must stay short.
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
	stage := filepath.Join(root, "source")
	example := filepath.Join(setting.AppWorkPath, "contrib/extensions/example")
	require.NoError(t, os.MkdirAll(stage, 0o700))
	require.NoError(t, os.CopyFS(filepath.Join(stage, "assets"), os.DirFS(filepath.Join(example, "assets"))))
	require.NoError(t, util.CopyFile(filepath.Join(example, "extension.json"), filepath.Join(stage, "extension.json")))
	require.NoError(t, util.CopyFile(filepath.Join(setting.AppWorkPath, ".artifacts/fountain-ci/example-backend"), filepath.Join(stage, "backend")))
	require.NoError(t, os.Chmod(filepath.Join(stage, "backend"), 0o700))
	installed := filepath.Join(root, "packages")
	store, err := packages.OpenPackageStore(installed)
	require.NoError(t, err)
	archive := fountainExampleArchive(t, stage)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)
	digest := sha256.Sum256(archive)
	entry := extension.CatalogPackage{
		ID: "example", Version: "1.0.0", URL: server.URL + "/example.tar.gz",
		Size: int64(len(archive)), SHA256: hex.EncodeToString(digest[:]),
		FountainProto: extension.Protocol, BrowserContract: extension.BrowserContractVersion,
		Capabilities: []string{extension.CapabilityActorRead},
	}
	verified, err := packages.AcquireWithClient(t.Context(), installed, entry, server.Client())
	require.NoError(t, err)
	_, err = store.InstallStaged(verified, packages.InstallOptions{Entry: &entry})
	require.NoError(t, err)
	_, err = store.SetDesired("example", true)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	catalog, err := json.Marshal(extension.Catalog{Version: extension.CatalogVersion, Packages: []extension.CatalogPackage{entry}})
	require.NoError(t, err)
	catalogPath := filepath.Join(root, "catalog.json")
	require.NoError(t, os.WriteFile(catalogPath, catalog, 0o600))

	previousSettings := setting.Extensions
	setting.Extensions.Enabled = true
	setting.Extensions.Path = installed
	setting.Extensions.RequiredIDs = nil
	manager := extensions_service.NewManager(installed)
	require.NoError(t, manager.SetCatalogPath(catalogPath))
	require.NoError(t, manager.SetCallbackHandlerFactory(web_extensions.CallbackHandlerForInstance))
	require.NoError(t, manager.SetInstanceStopped(web_extensions.RevokeAdmissionsForInstance))
	require.NoError(t, manager.Start(t.Context()))
	previous := extensions_service.GetManager()
	extensions_service.SetDefault(manager)
	t.Cleanup(func() {
		extensions_service.SetDefault(previous)
		require.NoError(t, manager.Close())
		setting.Extensions = previousSettings
	})
	require.Eventually(t, func() bool {
		return manager.Observe("example").State == packages.LifecycleEnabled
	}, 10*time.Second, 10*time.Millisecond, "example backend must complete its real startup handshake")
}

func fountainExampleArchive(t *testing.T, root string) []byte {
	t.Helper()
	var data bytes.Buffer
	compressed := gzip.NewWriter(&data)
	archive := tar.NewWriter(compressed)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(name)
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = archive.Write(body)
		return err
	}))
	require.NoError(t, archive.Close())
	require.NoError(t, compressed.Close())
	return data.Bytes()
}
