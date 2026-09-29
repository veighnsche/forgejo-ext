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
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "status", "sample"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "sample\tenabled\tmanifest-valid\n" {
		t.Fatalf("unexpected status: %q", got)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(`private-token`), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "status", "sample"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "sample\tenabled\tmanifest-invalid\n" || strings.Contains(got, "private-token") {
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
	app := &cli.Command{Commands: []*cli.Command{cmdExtensions()}}
	for _, action := range []string{"disable", "remove"} {
		if err := app.Run(context.Background(), []string{"forgejo", "extensions", action, "soda"}); err == nil {
			t.Fatalf("required package %s succeeded", action)
		}
	}
	if _, err := os.Stat(packageDir); err != nil {
		t.Fatalf("required package was removed: %v", err)
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
