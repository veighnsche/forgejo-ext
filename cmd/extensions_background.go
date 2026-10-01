// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package cmd

import (
	"context"
	"errors"
	"fmt"

	authmodel "forgejo.org/models/extensionauth"
	"forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"

	"github.com/urfave/cli/v3"
)

func extensionInstallationCommand() *cli.Command {
	return &cli.Command{Name: "installation", Usage: "Show a package's stable installation UUID", ArgsUsage: "ID", Action: func(_ context.Context, c *cli.Command) error {
		if c.Args().Len() != 1 {
			return errors.New("provide one extension id")
		}
		installation, err := extensions.LoadInstallation(setting.Extensions.Path, c.Args().First())
		if err != nil {
			return errors.New("installation identity unavailable")
		}
		_, err = fmt.Fprintf(c.Root().Writer, "%s\t%s\n", c.Args().First(), installation)
		return err
	}}
}

func backgroundBindingFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "installation", Usage: "Stable installation UUID from `extensions installation`"},
		&cli.Int64Flag{Name: "token-id", Usage: "Native personal access token ID"},
		&cli.Int64Flag{Name: "actor-id", Usage: "Expected stable native user ID"},
		&cli.Int64Flag{Name: "repository-id", Usage: "Stable native repository ID"},
		&cli.StringFlag{Name: "kind", Usage: "One of git.ref.publish, pull_request.create, pull_request.review.submit, pull_request.merge"},
	}
}

type backgroundBindingArgs struct {
	installation           string
	tokenID, actorID, repo int64
	kind                   string
}

func parseBackgroundBindingArgs(c *cli.Command) (backgroundBindingArgs, error) {
	args := backgroundBindingArgs{
		installation: c.String("installation"),
		tokenID:      c.Int64("token-id"),
		actorID:      c.Int64("actor-id"),
		repo:         c.Int64("repository-id"),
		kind:         c.String("kind"),
	}
	if args.installation == "" || args.tokenID <= 0 || args.actorID <= 0 || args.repo <= 0 || !authmodel.ValidKind(args.kind) {
		return backgroundBindingArgs{}, errors.New("provide --installation, --token-id, --actor-id, --repository-id and a supported --kind")
	}
	return args, nil
}

func extensionBindCommand() *cli.Command {
	return &cli.Command{
		Name: "bind", Usage: "Enroll a background actor binding (live database, safe while running)", Flags: backgroundBindingFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			args, err := parseBackgroundBindingArgs(c)
			if err != nil {
				return err
			}
			ctx, cancel := installSignals(ctx)
			defer cancel()
			if err := initDB(ctx); err != nil {
				return err
			}
			binding, err := authmodel.EnrollBinding(ctx, args.installation, args.tokenID, args.actorID, args.repo, args.kind)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(c.Root().Writer, "binding %d enrolled for installation %s.\n", binding.ID, binding.InstallationID)
			return err
		},
	}
}

func extensionBindingsCommand() *cli.Command {
	return &cli.Command{
		Name: "bindings", Usage: "List an installation's background actor bindings (IDs only, never secrets)",
		Flags: []cli.Flag{&cli.StringFlag{Name: "installation", Usage: "Stable installation UUID"}},
		Action: func(ctx context.Context, c *cli.Command) error {
			installation := c.String("installation")
			if installation == "" {
				return errors.New("provide --installation")
			}
			ctx, cancel := installSignals(ctx)
			defer cancel()
			if err := initDB(ctx); err != nil {
				return err
			}
			bindings, err := authmodel.ListBindings(ctx, installation)
			if err != nil {
				return err
			}
			for _, binding := range bindings {
				if _, err := fmt.Fprintf(c.Root().Writer, "%d\ttoken=%d\tactor=%d\trepository=%d\tkind=%s\n",
					binding.ID, binding.TokenID, binding.ActorID, binding.RepositoryID, binding.Kind); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func extensionUnbindCommand() *cli.Command {
	return &cli.Command{
		Name: "unbind", Usage: "Revoke a background actor binding, ending new admission", Flags: backgroundBindingFlags(),
		Action: func(ctx context.Context, c *cli.Command) error {
			args, err := parseBackgroundBindingArgs(c)
			if err != nil {
				return err
			}
			ctx, cancel := installSignals(ctx)
			defer cancel()
			if err := initDB(ctx); err != nil {
				return err
			}
			revoked, err := authmodel.RevokeBinding(ctx, args.installation, args.tokenID, args.actorID, args.repo, args.kind)
			if err != nil {
				return err
			}
			if !revoked {
				return errors.New("no such binding")
			}
			_, err = fmt.Fprintln(c.Root().Writer, "binding revoked; new submissions under it are refused.")
			return err
		},
	}
}
