// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Command r03-consumer is the R03 independently built SDK consumer fixture:
// a standalone extension binary compiled from this source against the
// canonical SDK (resolved through the parent module's replace directive to
// the clean-tree sdk bytes, never a published version or a copied checkout).
// It carries no test scaffolding: unlike the in-package test helper, it has
// no TestMain branch and ignores FORGEJO_EXTENSION_TEST_HELPER. The manager
// spawns it as an ordinary package executable; a successful service
// bootstrap through it proves the real extension manager/package handshake.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	sdk "forgejo.org/extension-sdk"
)

// Witness environment (set by the orchestrating test through the package
// wrapper script, mirroring operator provisioning of the shared service
// socket). Absent R03_WITNESS_SERVICE_SOCKET the consumer keeps the plain
// handshake-only behavior used by the admission witness.
const (
	witnessServiceSocketEnv = "R03_WITNESS_SERVICE_SOCKET"
	witnessExpectedUIDEnv   = "R03_WITNESS_EXPECTED_UID"
	witnessGenerationEnv    = "R03_WITNESS_EXPECTED_GENERATION"
	witnessResultFileEnv    = "R03_WITNESS_RESULT_FILE"
	witnessRotateFileEnv    = "R03_WITNESS_ROTATE_FILE"
)

type witnessOutcome struct {
	BootstrapOK      bool   `json:"bootstrap_ok"`
	OperationsOK     bool   `json:"operations_ok"`
	Generation       string `json:"generation"`
	MismatchRefused  bool   `json:"mismatch_refused"`
	HeldAdmissionOK  bool   `json:"held_admission_ok"`
	RotatedOpRefused bool   `json:"rotated_op_refused"`
	RotatedOpError   string `json:"rotated_op_error"`
	Failure          string `json:"failure,omitempty"`
}

func (o *witnessOutcome) fail(format string, args ...any) {
	o.Failure = fmt.Sprintf(format, args...)
}

func runWitness(serviceSocket string, expectedUID uint64, expectedGeneration, resultFile, rotateFile string) {
	outcome := &witnessOutcome{}
	done := func() {
		data, err := json.Marshal(outcome)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		// Publish a complete snapshot; the parent may read concurrently.
		temporary := resultFile + ".tmp"
		if err := os.WriteFile(temporary, data, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		if err := os.Rename(temporary, resultFile); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// The manager handshake completes asynchronously after spawn: retry
	// bootstrap until the host admits this backend or the budget ends.
	var client sdk.BackgroundClient
	bootstrapped := false
	bootstrapDeadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(bootstrapDeadline) {
		candidate, err := sdk.BootstrapServiceBackground(ctx, sdk.ServiceBridgeOptions{
			SocketPath: serviceSocket, ExpectedHostUID: uint32(expectedUID),
		})
		if err == nil {
			client = candidate
			bootstrapped = true
			break
		}
		select {
		case <-ctx.Done():
			outcome.fail("bootstrap: %v", err)
			done()
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !bootstrapped {
		outcome.fail("bootstrap never succeeded within budget")
		done()
		return
	}
	outcome.BootstrapOK = true
	handshake, err := client.ReadOperations(ctx)
	if err != nil {
		outcome.fail("read operations: %v", err)
		done()
		return
	}
	outcome.OperationsOK = true
	outcome.Generation = handshake.Generation
	if handshake.Generation != expectedGeneration {
		outcome.fail("operations generation %q, want %q", handshake.Generation, expectedGeneration)
		done()
		return
	}
	// A pinned wrong installation must be refused without touching the held
	// admission.
	if _, err := sdk.BootstrapServiceBackground(ctx, sdk.ServiceBridgeOptions{
		SocketPath: serviceSocket, ExpectedHostUID: uint32(expectedUID),
		InstallationID: "22222222-2222-4333-8444-555555555555",
	}); err == nil || err.Error() != "background bootstrap returned HTTP 403" {
		outcome.fail("wrong installation bootstrap: got %v, want HTTP 403", err)
		done()
		return
	}
	outcome.MismatchRefused = true
	// The rejected bootstrap must leave the existing admission live.
	// Otherwise a later 401 would not prove the orchestrator's rotation.
	held, err := client.ReadOperations(ctx)
	if err != nil || held.Generation != expectedGeneration {
		outcome.fail("held admission after rejected bootstrap: %v (generation %q)", err, held.Generation)
		done()
		return
	}
	outcome.HeldAdmissionOK = true
	// Interim outcome: the orchestrator reads this, rotates the admission,
	// then creates the rotate file. The held admission must stop working.
	done()
	rotated := false
	for i := 0; i < 250; i++ {
		if _, err := os.Stat(rotateFile); err == nil {
			rotated = true
			break
		}
		select {
		case <-ctx.Done():
			outcome.fail("rotate signal never arrived")
			done()
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	if !rotated {
		outcome.fail("rotate signal never arrived")
		done()
		return
	}
	if _, err := client.ReadOperations(ctx); err == nil {
		outcome.fail("operations with rotated admission unexpectedly succeeded")
		done()
		return
	} else {
		outcome.RotatedOpError = err.Error()
	}
	outcome.RotatedOpRefused = true
	done()
}

func main() {
	application := sdk.Application{HTTP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authority, err := sdk.RequestContext(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, authority.ExtensionID+":"+authority.Actor.Username)
	})}
	if serviceSocket := os.Getenv(witnessServiceSocketEnv); serviceSocket != "" {
		expectedUID, err := strconv.ParseUint(os.Getenv(witnessExpectedUIDEnv), 10, 32)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		go runWitness(serviceSocket, expectedUID, os.Getenv(witnessGenerationEnv), os.Getenv(witnessResultFileEnv), os.Getenv(witnessRotateFileEnv))
	}
	if err := sdk.Serve(application); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
