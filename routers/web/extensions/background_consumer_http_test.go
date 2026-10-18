// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	extensionauth_model "forgejo.org/models/extensionauth"
	packages "forgejo.org/modules/extensions"
	extensions_service "forgejo.org/services/extensions"

	"github.com/stretchr/testify/require"
)

// shortWitnessRoot creates a short-lived root with a short random name.
// Scratch stays under TMPDIR (or the home cache when TMPDIR is empty);
// never under /tmp or the root disk.
func shortWitnessRoot(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TMPDIR")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".cache", "forgejo-test-tmp")
	}
	require.NoError(t, os.MkdirAll(base, 0o700))
	root, err := os.MkdirTemp(base, "r03w-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

// This witness drives an actual SDK background HTTP operation from the
// independently built consumer through the real background mux: bootstrap
// over the shared service socket with peer-credential and installation
// checks, one admitted operations read, a refused wrong-installation pin,
// and consumer-observed invalidation after the orchestrator rotates the
// admission. Operation execution itself stays stubbed through the fake
// operations service, stated explicitly: this proves the admitted channel,
// never native external effects.
func TestIndependentConsumerBackgroundHTTP(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	pkgDir := filepath.Dir(thisFile)
	moduleRoot := filepath.Join(pkgDir, "..", "..", "..")
	consumerSrc := filepath.Join(moduleRoot, "services", "extensions", "testdata", "r03-consumer", "main.go")
	before, err := os.ReadFile(consumerSrc)
	require.NoError(t, err)

	// Unix socket paths cap at 108 bytes: never nest the runtime tree
	// under the test-name directory.
	root := shortWitnessRoot(t)
	binary := filepath.Join(root, "r03-consumer")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", binary, "./services/extensions/testdata/r03-consumer")
	build.Dir = moduleRoot
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=-mod=readonly")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "independent consumer build failed: %s", output)
	after, err := os.ReadFile(consumerSrc)
	require.NoError(t, err)
	require.Equal(t, before, after, "consumer inputs must be unchanged by the build")

	const packageID = "r03httpconsumer"
	packageDir := filepath.Join(root, packageID)
	require.NoError(t, os.MkdirAll(packageDir, 0o700))
	self := uint32(os.Geteuid())
	operations := testOperations()
	serviceSocket := filepath.Join(root, "service.sock")
	resultFile := filepath.Join(root, "witness-result.json")
	rotateFile := filepath.Join(root, "witness-rotated")
	backendLog := filepath.Join(root, "backend-stderr.log")
	run := fmt.Sprintf("#!/bin/sh\nexport R03_WITNESS_SERVICE_SOCKET=%q R03_WITNESS_EXPECTED_UID=%d R03_WITNESS_EXPECTED_GENERATION=%q R03_WITNESS_RESULT_FILE=%q R03_WITNESS_ROTATE_FILE=%q\nexec %q 2>%q\n",
		serviceSocket, self, operations.handshake.Generation, resultFile, rotateFile, binary, backendLog)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "run"), []byte(run), 0o700))
	manifest := fmt.Sprintf(`{"protocol":1,"id":%q,"name":"R03HTTPIndependent","version":"1.0","executable":"run","capabilities":["native.service.bridge","native.background.operations"]}`, packageID)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(manifest), 0o600))

	manager := extensions_service.NewManager(root)
	require.NoError(t, manager.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	}))
	browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	verifiers := backgroundVerifiers{
		submit: func(context.Context, extensionauth_model.SubmissionRequest) (extensionauth_model.SubmissionDecision, error) {
			return extensionauth_model.SubmissionDecision{}, errors.New("stubbed execution: no submit in this witness")
		},
		snapshot: func(context.Context, extensionauth_model.SnapshotReadRequest) (extensionauth_model.SnapshotReadDecision, error) {
			return extensionauth_model.SnapshotReadDecision{}, errors.New("stubbed execution: no snapshot in this witness")
		},
	}
	require.NoError(t, manager.SetServiceCallbackEndpoint(serviceSocket, backgroundMux(manager, browser, "", true, verifiers, operations)))
	require.NoError(t, manager.SetServiceBridgePeers(map[uint32]string{self: packageID}))

	store, err := packages.OpenPackageStore(root)
	require.NoError(t, err)
	_, err = store.SetDesired(packageID, true)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(func() { _ = manager.Close() })
	deadline := time.Now().Add(25 * time.Second)
	for {
		if _, _, ok := manager.Lookup(packageID); ok {
			break
		}
		if time.Now().After(deadline) {
			observed := manager.Observe(packageID)
			t.Logf("independent consumer observe error: %+v", observed.Error)
			backendContents, _ := os.ReadFile(backendLog)
			t.Logf("independent consumer stderr: %s", string(backendContents))
			t.Fatalf("independent consumer did not start: %+v", observed)
		}
		time.Sleep(200 * time.Millisecond)
	}
	installation, err := packages.LoadInstallation(root, packageID)
	require.NoError(t, err)
	require.NotEmpty(t, installation)

	// The consumer bootstraps itself, reads operations, refuses the wrong
	// pin, then waits for the rotation signal below.
	outcomeDeadline := time.Now().Add(45 * time.Second)
	for {
		if data, err := os.ReadFile(resultFile); err == nil {
			var outcome map[string]any
			require.NoError(t, json.Unmarshal(data, &outcome))
			if failure, _ := outcome["failure"].(string); failure != "" {
				t.Fatalf("consumer witness failed before rotation: %s", failure)
			}
			require.Equal(t, true, outcome["bootstrap_ok"])
			require.Equal(t, true, outcome["operations_ok"])
			require.Equal(t, operations.handshake.Generation, outcome["generation"])
			require.Equal(t, true, outcome["mismatch_refused"])
			break
		}
		if time.Now().After(outcomeDeadline) {
			t.Fatal("consumer witness produced no outcome")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Rotate the service admission out from under the consumer, then let it
	// observe the refusal on its held admission.
	rotated, _, err := manager.BootstrapServiceAdmission(self, installation)
	require.NoError(t, err)
	require.NotEmpty(t, rotated)
	require.NoError(t, os.WriteFile(rotateFile, []byte("rotated\n"), 0o600))
	rotatedDeadline := time.Now().Add(30 * time.Second)
	for {
		data, err := os.ReadFile(resultFile)
		require.NoError(t, err)
		var outcome map[string]any
		require.NoError(t, json.Unmarshal(data, &outcome))
		if failure, _ := outcome["failure"].(string); failure != "" {
			t.Fatalf("consumer witness failed after rotation: %s", failure)
		}
		if refused, _ := outcome["rotated_op_refused"].(bool); refused {
			require.Contains(t, outcome["rotated_op_error"], "401")
			return
		}
		if time.Now().After(rotatedDeadline) {
			t.Fatal("consumer never observed rotation invalidation")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
