// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"

	"github.com/urfave/cli/v3"
)

func cmdExtensions() *cli.Command {
	return &cli.Command{
		Name:  "extensions",
		Usage: "Manage trusted extension packages (stop Forgejo before changes)",
		Commands: []*cli.Command{
			{
				Name: "install", Usage: "Install a prebuilt package directory", ArgsUsage: "DIRECTORY",
				Flags: []cli.Flag{&cli.BoolFlag{Name: "replace", Usage: "Replace an installed package, preserving activation and data"}},
				Action: func(_ context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return errors.New("provide one prebuilt package directory")
					}
					m, err := extensions.Install(setting.Extensions.Path, c.Args().First(), c.Bool("replace"))
					if err != nil {
						return err
					}
					_, err = fmt.Fprintf(c.Root().Writer, "Installed %s %s. Start Forgejo with [extensions] ENABLED = true to activate enabled packages.\n", m.ID, safeExtensionVersion(m.Version))
					return err
				},
			},
			{Name: "list", Usage: "List installed packages and next-start activation", Action: listExtensions},
			{Name: "status", Usage: "Show package validation and next-start activation", ArgsUsage: "ID", Action: extensionStatus},
			{Name: "remove", Usage: "Remove a stopped package while retaining its private data", ArgsUsage: "ID", Action: func(_ context.Context, c *cli.Command) error {
				if c.Args().Len() != 1 {
					return errors.New("provide one extension id")
				}
				id := c.Args().First()
				if slices.Contains(setting.Extensions.RequiredIDs, id) {
					return errors.New("required extension cannot be removed")
				}
				if err := extensions.Remove(setting.Extensions.Path, id); err != nil {
					return err
				}
				_, err := fmt.Fprintf(c.Root().Writer, "%s: package removed; private data retained.\n", id)
				return err
			}},
			extensionActivationCommand("enable", true),
			extensionActivationCommand("disable", false),
		},
	}
}

func extensionStatus(_ context.Context, c *cli.Command) error {
	if c.Args().Len() != 1 || !sdk.ValidID(c.Args().First()) {
		return errors.New("provide one valid extension id")
	}
	id := c.Args().First()
	directory := filepath.Join(setting.Extensions.Path, id)
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("installed package is not a regular directory")
	}
	state := "enabled"
	if _, err := os.Lstat(filepath.Join(directory, ".disabled")); err == nil {
		state = "disabled"
	} else if !errors.Is(err, fs.ErrNotExist) {
		return errors.New("cannot read package activation state")
	}
	validation := "manifest-valid"
	if m, err := sdk.LoadManifest(directory); err != nil || m.ID != id {
		validation = "manifest-invalid"
	}
	_, err = fmt.Fprintf(c.Root().Writer, "%s\t%s\t%s\n", id, state, validation)
	return err
}

func extensionActivationCommand(name string, enabled bool) *cli.Command {
	return &cli.Command{Name: name, Usage: name + " a package for the next Forgejo start", ArgsUsage: "ID", Action: func(_ context.Context, c *cli.Command) error {
		if c.Args().Len() != 1 {
			return errors.New("provide one extension id")
		}
		if !enabled && slices.Contains(setting.Extensions.RequiredIDs, c.Args().First()) {
			return errors.New("required extension cannot be disabled")
		}
		if err := extensions.SetEnabled(setting.Extensions.Path, c.Args().First(), enabled); err != nil {
			return err
		}
		_, err := fmt.Fprintf(c.Root().Writer, "%s: %s takes effect at the next start.\n", c.Args().First(), name)
		return err
	}}
}

func listExtensions(_ context.Context, c *cli.Command) error {
	if c.Args().Len() != 0 {
		return errors.New("list does not accept arguments")
	}
	entries, err := os.ReadDir(setting.Extensions.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if !sdk.ValidID(id) {
			return errors.New("invalid package directory name")
		}
		state := "enabled"
		if _, err := os.Lstat(filepath.Join(setting.Extensions.Path, id, ".disabled")); err == nil {
			state = "disabled"
		} else if !errors.Is(err, fs.ErrNotExist) {
			return errors.New("cannot read package activation state")
		}
		version := "-"
		if m, err := sdk.LoadManifest(filepath.Join(setting.Extensions.Path, id)); err != nil || m.ID != id {
			state = "invalid"
		} else {
			version = safeExtensionVersion(m.Version)
		}
		if _, err := fmt.Fprintf(c.Root().Writer, "%s\t%s\t%s\n", id, version, state); err != nil {
			return err
		}
	}
	return nil
}

func safeExtensionVersion(version string) string {
	if len(version) == 0 || len(version) > 64 {
		return "-"
	}
	for _, char := range version {
		if !(char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char == '.' || char == '_' || char == '+' || char == '-') {
			return "-"
		}
	}
	return version
}
