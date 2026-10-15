// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"

	"github.com/urfave/cli/v3"
)

func TestExtensionStatusAndRemove(t *testing.T) {
	root := t.TempDir()
	previous := setting.Extensions.Path
	setting.Extensions.Path = root
	t.Cleanup(func() { setting.Extensions.Path = previous })
	packageDir := filepath.Join(root, "sample")
	if err := os.Mkdir(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(`{"protocol":1,"id":"sample","name":"Sample","version":"1","executable":"backend"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, ".data", "sample")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "secret"), []byte("private-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app := &cli.Command{Commands: []*cli.Command{cmdExtensions()}, Writer: &output}
	// An absent intent reads disabled; CLI activation persists an intent.
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "status", "sample"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "sample\tdisabled\tmanifest-valid\n" {
		t.Fatalf("unexpected status: %q", got)
	}
	output.Reset()
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "enable", "sample"}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "status", "sample"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "sample\tenabled\tmanifest-valid\n" {
		t.Fatalf("unexpected enabled package status: %q", got)
	}
	output.Reset()
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "list"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "sample\t1\tenabled\n" {
		t.Fatalf("unexpected enabled package listing: %q", got)
	}
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "disable", "sample"}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "status", "sample"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "sample\tdisabled\tmanifest-valid\n" {
		t.Fatalf("unexpected disabled package status: %q", got)
	}
	output.Reset()
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "list"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "sample\t1\tdisabled\n" {
		t.Fatalf("unexpected disabled package listing: %q", got)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(`private-token`), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "status", "sample"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "sample\tdisabled\tmanifest-invalid\n" || strings.Contains(got, "private-token") {
		t.Fatalf("unsafe status: %q", got)
	}
	output.Reset()
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "list"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "sample\t-\tinvalid\n" {
		t.Fatalf("unsafe package listing: %q", got)
	}
	output.Reset()
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "remove", "sample"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(packageDir); !os.IsNotExist(err) {
		t.Fatalf("package remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "secret")); err != nil {
		t.Fatalf("private data removed: %v", err)
	}
}

func TestPrepareRequiresOfflineAndRefusesMissingCache(t *testing.T) {
	root := t.TempDir()
	previous := setting.Extensions
	setting.Extensions.Path = root
	setting.Extensions.RequiredIDs = []string{"soda"}
	t.Cleanup(func() { setting.Extensions = previous })
	catalog := `{"version":1,"packages":[{"id":"soda","version":"1.0.0","url":"https://releases.example.invalid/soda.tar.gz","size":1024,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","required":true,"fountain_protocol":1,"browser_contract":1,"capabilities":[]}]}`
	catalogPath := filepath.Join(root, "catalog.json")
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := `{"catalog_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","entries":[{"package_id":"soda","installation_id":null,"generation":"0","desired_enabled":true,"pending_operation":null,"installed_version":null,"package_sha256":null,"required":true}]}`
	snapshotPath := filepath.Join(root, "snapshot.json")
	if err := os.WriteFile(snapshotPath, []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &cli.Command{Commands: []*cli.Command{cmdExtensions()}}
	// --offline is mandatory: there is no network fallback.
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "prepare", "--catalog", catalogPath, "--snapshot", snapshotPath}); err == nil {
		t.Fatal("online prepare succeeded")
	}
	// A missing cached target is refused without networking.
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "prepare", "--offline", "--catalog", catalogPath, "--snapshot", snapshotPath}); err == nil {
		t.Fatal("prepare without cached packages succeeded")
	}
}

func TestRequiredExtensionsCannotBeDisabledOrRemoved(t *testing.T) {
	root := t.TempDir()
	previous := setting.Extensions
	setting.Extensions.Path = root
	setting.Extensions.RequiredIDs = []string{"soda"}
	t.Cleanup(func() { setting.Extensions = previous })
	packageDir := filepath.Join(root, "soda")
	if err := os.Mkdir(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(`{"protocol":1,"id":"soda","name":"Soda","version":"1","executable":"backend"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &cli.Command{Commands: []*cli.Command{cmdExtensions()}}
	for _, action := range []string{"disable", "remove"} {
		if err := app.Run(context.Background(), []string{"forgejo", "extensions", action, "soda"}); err == nil {
			t.Fatalf("required package %s succeeded", action)
		}
	}
	if _, err := os.Stat(packageDir); err != nil {
		t.Fatalf("required package was removed: %v", err)
	}
	// The refused disable persists no requested state behind it.
	if _, err := os.Stat(filepath.Join(root, ".intents", "soda.json")); !os.IsNotExist(err) {
		t.Fatalf("required package was disabled: %v", err)
	}
}

func TestCLIRequiredExtensionsFailClosedWhenDisabledOrWebOwnsLock(t *testing.T) {
	root := t.TempDir()
	previous := setting.Extensions
	setting.Extensions.Path = root
	setting.Extensions.RequiredIDs = []string{"soda"}
	t.Cleanup(func() { setting.Extensions = previous })
	if _, err := startExtensionsForCLI(context.Background()); err == nil {
		t.Fatal("disabled required extensions were accepted")
	}
	setting.Extensions.Enabled = true
	lock, err := extensions.AcquirePackageLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := startExtensionsForCLI(context.Background()); err == nil {
		t.Fatal("CLI bypassed a web-owned package lock")
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := startExtensionsForCLI(context.Background()); err == nil {
		t.Fatal("CLI accepted a missing required package")
	}
}
