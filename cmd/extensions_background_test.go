// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"

	"github.com/urfave/cli/v3"
)

func TestExtensionInstallationShowsUUID(t *testing.T) {
	root := t.TempDir()
	previous := setting.Extensions.Path
	setting.Extensions.Path = root
	t.Cleanup(func() { setting.Extensions.Path = previous })
	installation, err := extensions.EnsureInstallation(root, "sample")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app := &cli.Command{Commands: []*cli.Command{cmdExtensions()}, Writer: &output}
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "installation", "sample"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "sample\t"+installation+"\n" {
		t.Fatalf("unexpected installation output: %q", got)
	}
	output.Reset()
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "installation", "missing"}); err == nil {
		t.Fatal("missing installation identity must fail")
	}
}

func TestExtensionBindingCommandsValidateArguments(t *testing.T) {
	root := t.TempDir()
	previous := setting.Extensions.Path
	setting.Extensions.Path = root
	t.Cleanup(func() { setting.Extensions.Path = previous })
	_ = os.MkdirAll(filepath.Join(root, ".installations"), 0o700)

	var output bytes.Buffer
	app := &cli.Command{Commands: []*cli.Command{cmdExtensions()}, Writer: &output}
	// Argument validation fails before any database access.
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "bind", "--installation", "x"}); err == nil {
		t.Fatal("incomplete bind arguments must fail")
	}
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "bind",
		"--installation", "11111111-2222-4333-8444-555555555555",
		"--token-id", "1", "--actor-id", "2", "--repository-id", "3",
		"--kind", "pull_request.delete"}); err == nil {
		t.Fatal("unknown bind kind must fail")
	}
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "unbind"}); err == nil {
		t.Fatal("incomplete unbind arguments must fail")
	}
	if err := app.Run(context.Background(), []string{"forgejo", "extensions", "bindings"}); err == nil {
		t.Fatal("bindings without installation must fail")
	}
}
