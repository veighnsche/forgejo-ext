// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package cmd

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
	operation_service "forgejo.org/services/nativeoperation"

	"github.com/urfave/cli/v3"
)

// exitFenced is the process exit code when offline recovery refuses to
// release: the owner stays held for intervention. Released and idle both
// exit 0; usage and infrastructure failures exit 1.
const exitFenced = 3

func subcmdNativeOperation() *cli.Command {
	return &cli.Command{
		Name:  "native-operation",
		Usage: "Offline native-mutation recovery for the host operator",
		Commands: []*cli.Command{
			{
				Name:  "recover",
				Usage: "Reconcile one held owner after whole-domain stop and restart inhibition",
				Description: `Reconcile the held native-mutation owner offline and release it only
when its exact owner and generation match and its effect is known from
authoritative evidence.

Before running this command, stop every native writer for the data set
through the deployment's service controls (HTTP/SSH ingress, web workers,
queue workers, scheduled and admin commands, surviving descendants) and
inhibit restart until reconciliation finishes. With the domain stopped,
create the offline marker, run this command naming the exact held owner
and generation, verify the verdict, then lift inhibition before
restarting writers.

Wrong owners, stale generations, unaccounted ordinary effects and
uncertain attribution stay fenced for intervention. There is no
force-unlock flag and no timeout release.`,
				Before: noDanglingArgs,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "owner",
						Usage:    "Exact reservation owner to reconcile (required)",
						Required: true,
					},
					&cli.Int64Flag{
						Name:     "generation",
						Usage:    "Exact fencing generation to reconcile (required)",
						Required: true,
					},
				},
				Action: runNativeOperationRecover,
			},
			{
				Name:  "status",
				Usage: "Show the bounded reservation state for offline recovery",
				Description: `Show the reservation state the host operator needs for
offline recovery: idle or held owner with its kind, family and
generation, the native revision, and restart inhibition. The
output never includes verifier secrets or scope payloads, and
this command never claims or releases the reservation. There is
no force-unlock flag and no timeout release.`,
				Before: noDanglingArgs,
				Action: runNativeOperationStatus,
			},
		},
	}
}

func runNativeOperationRecover(ctx context.Context, c *cli.Command) error {
	// Full settings: reconciliation reads repository paths as well as the
	// database, and git must be initialized for ref evidence.
	setting.LoadSettings()

	ctx, cancel := installSignals(ctx)
	defer cancel()

	if err := initDB(ctx); err != nil {
		return err
	}
	if err := git.InitSimple(ctx); err != nil {
		return err
	}

	owner := c.String("owner")
	generation := c.Int64("generation")
	if owner == "" || generation <= 0 {
		return errors.New("recover requires --owner and a positive --generation naming the exact held owner")
	}

	assessment, err := operation_service.Default().Recover(ctx, owner, generation)
	if err != nil {
		if errors.Is(err, operation_service.ErrNotInhibited) {
			return cli.Exit("domain is not inhibited: stop every native writer and inhibit restart before reconciling", 1)
		}
		return err
	}

	fmt.Printf("owner: %s\n", assessment.Owner)
	fmt.Printf("generation: %d\n", assessment.Generation)
	fmt.Printf("kind: %s\n", assessment.OwnerKind)
	fmt.Printf("family: %s\n", assessment.Family)
	fmt.Printf("verdict: %s\n", assessment.Verdict)
	if assessment.Effect != "" {
		fmt.Printf("effect: %s\n", assessment.Effect)
	}
	if assessment.Reason != "" {
		fmt.Printf("reason: %s\n", assessment.Reason)
	}
	for _, check := range assessment.Checks {
		fmt.Printf("check: %s\n", check)
	}

	switch assessment.Verdict {
	case operation_service.RecoveryReleased, operation_service.RecoveryIdle:
		return nil
	default:
		return cli.Exit("owner stays fenced for intervention", exitFenced)
	}
}

func runNativeOperationStatus(ctx context.Context, c *cli.Command) error {
	// Full settings: inhibition is read from the deployment's data path.
	setting.LoadSettings()

	ctx, cancel := installSignals(ctx)
	defer cancel()

	if err := initDB(ctx); err != nil {
		return err
	}

	status, err := operation_service.Default().Status(ctx)
	if err != nil {
		return err
	}

	if status.Idle {
		fmt.Println("state: idle")
	} else {
		fmt.Println("state: held")
	}
	fmt.Printf("revision: %d\n", status.Revision)
	if !status.Idle {
		fmt.Printf("owner: %s\n", status.Owner)
		fmt.Printf("generation: %d\n", status.Generation)
		if status.Kind != "" {
			fmt.Printf("kind: %s\n", status.Kind)
		}
		if status.Family != "" {
			fmt.Printf("family: %s\n", status.Family)
		}
	}
	fmt.Printf("inhibited: %t\n", status.Inhibited)
	return nil
}
