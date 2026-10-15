// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/require"
)

// setDesiredState records next-start activation for an installed package
// through the shared package store. Manifest-less directories use
// persistDesiredState instead: SetDesired validates the manifest.
func setDesiredState(t *testing.T, root, id string, desired bool) packages.Intent {
	t.Helper()
	store, err := packages.OpenPackageStore(root)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	intent, err := store.SetDesired(id, desired)
	require.NoError(t, err)
	return intent
}

// persistDesiredState records requested state without manifest
// validation, for startup fixtures with broken or missing packages.
func persistDesiredState(t *testing.T, root, id string, desired bool) packages.Intent {
	t.Helper()
	installation, err := packages.EnsureInstallation(root, id)
	require.NoError(t, err)
	intent, err := packages.StoreIntent(root, id, installation, desired, nil, "0")
	require.NoError(t, err)
	return intent
}

// testCatalogEntry builds one structurally valid catalog entry for the
// manager's configured catalog fixture.
func testCatalogEntry(id, version string, required bool, controller string) sdk.CatalogPackage {
	return sdk.CatalogPackage{
		ID: id, Version: version, URL: "https://catalog.example.invalid/" + id + ".tar.gz",
		Size: 1024, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Required: required, FountainProto: sdk.Protocol, BrowserContract: sdk.BrowserContractVersion,
		Capabilities: []string{"native.actor.read"}, LifecycleController: controller,
	}
}

// configureTestCatalog writes an exact catalog fixture and points the
// manager at it before Start. Required layouts always configure one:
// a required package with no catalog fails closed at startup.
func configureTestCatalog(t *testing.T, m *Manager, root string, entries ...sdk.CatalogPackage) {
	t.Helper()
	data, err := json.Marshal(sdk.Catalog{Version: sdk.CatalogVersion, Packages: entries})
	require.NoError(t, err)
	path := filepath.Join(root, ".test-catalog.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	require.NoError(t, m.SetCatalogPath(path))
}

func TestMain(m *testing.M) {
	if os.Getenv("FORGEJO_EXTENSION_TEST_HELPER") == "1" {
		os.Exit(runTestHelper())
	}
	os.Exit(m.Run())
}

// testFixtureKnobs steer the re-executed helper process per package through
// its private data directory. There is one helper binary for every
// backend; the knob file tells it which role to play.
type testFixtureKnobs struct {
	Role           string `json:"role"`
	FailActivate   bool   `json:"fail_activate"`
	SlowActivateMs int    `json:"slow_activate_ms"`
	GateActivate   bool   `json:"gate_activate,omitempty"`
	SlowStartMs    int    `json:"slow_start_ms"`
	CrashAfterMs   int    `json:"crash_after_ms"`
}

func readTestKnobs() testFixtureKnobs {
	var knobs testFixtureKnobs
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), "fixture.json"))
	if err != nil {
		return knobs
	}
	_ = json.Unmarshal(data, &knobs)
	return knobs
}

func runTestHelper() int {
	if dir := os.Getenv("FORGEJO_EXTENSION_TEST_HOLDDIR"); dir != "" {
		return holdTestLock(dir)
	}
	knobs := readTestKnobs()
	if knobs.SlowStartMs > 0 {
		time.Sleep(time.Duration(knobs.SlowStartMs) * time.Millisecond)
	}
	switch knobs.Role {
	case "plain-exit":
		os.Exit(1)
	case "controller":
		return runControllerHelper(knobs)
	}
	if knobs.CrashAfterMs > 0 {
		go func() {
			time.Sleep(time.Duration(knobs.CrashAfterMs) * time.Millisecond)
			os.Exit(1)
		}()
	}
	application := sdk.Application{HTTP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authority, err := sdk.RequestContext(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, authority.ExtensionID+":"+authority.Actor.Username)
	})}
	if policy := os.Getenv("FORGEJO_EXTENSION_TEST_POLICY"); policy != "" {
		application.Policies = map[string]sdk.PolicyHandler{sdk.PolicyForgejoUsername: func(ctx context.Context, _ sdk.PolicyRequest) (sdk.PolicyDecision, error) {
			if policy == "timeout" {
				<-ctx.Done()
				return sdk.PolicyDecision{}, ctx.Err()
			}
			return sdk.PolicyDecision{Allowed: policy == "allow", ReasonCode: "rejected"}, nil
		}}
	}
	if err := sdk.Serve(application); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func installTestPackage(t *testing.T, root, id string) {
	t.Helper()
	packageDir := filepath.Join(root, id)
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "extension"), bytes, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"protocol":1,"id":%q,"name":"Test","version":"1.0","executable":"extension"}`, id)
	if err := os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

// installCatalogPackage installs one fixture backend whose manifest and
// generated receipt exactly match the catalog entry: production
// installs always write receipts, and required bootstrap reuses only
// exact receipt, manifest, capability, and controller matches. extra
// carries additional manifest fields such as policies.
func installCatalogPackage(t *testing.T, root string, entry sdk.CatalogPackage, extra map[string]any) {
	t.Helper()
	packageDir := filepath.Join(root, entry.ID)
	require.NoError(t, os.MkdirAll(packageDir, 0o700))
	executable, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(executable)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension"), data, 0o700))
	manifest := map[string]any{
		"protocol": 1, "id": entry.ID, "name": "Test", "version": entry.Version,
		"executable": "extension", "capabilities": entry.Capabilities,
	}
	if entry.LifecycleController != "" {
		manifest["lifecycle_controller"] = entry.LifecycleController
	}
	maps.Copy(manifest, extra)
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), encoded, 0o600))
	require.NoError(t, packages.WriteReceipt(packageDir, entry))
}

//nolint:usetesting // Keep Unix socket paths short or retain the external fixture root.
func shortTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(os.TempDir(), "e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func waitStartupFinished(t *testing.T, m *Manager) {
	t.Helper()
	m.mu.Lock()
	done := m.startupDone
	m.mu.Unlock()
	require.NotNil(t, done)
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("optional startup pass did not finish")
	}
}

func TestManagerNativeHTTPAndCrashCleanup(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	setDesiredState(t, root, "sample", true)
	m := NewManager(root)
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitStartupFinished(t, m)
	defer m.Close()
	if lock, err := packages.AcquirePackageLock(root); err == nil {
		_ = lock.Close()
		t.Fatal("package mutation lock was available while extension was running")
	}
	if _, err := packages.OpenPackageStore(root); err == nil {
		t.Fatal("package store opened while extension was running")
	}
	list := m.List()
	if len(list) != 1 || list[0].Manifest.ID != "sample" {
		t.Fatalf("unexpected registry: %+v", list)
	}
	list[0].Manifest.ID = "changed"
	if m.List()[0].Manifest.ID != "sample" {
		t.Fatal("registry returned mutable manifest")
	}
	_, transport, ok := m.Lookup("sample")
	if !ok {
		t.Fatal("running extension missing")
	}
	request, err := http.NewRequest(http.MethodGet, "http://extension/hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := json.Marshal(sdk.Authority{ExtensionID: "sample", InstanceID: "instance", SessionGeneration: "generation", Contribution: sdk.Contribution{ID: "sample", Kind: "page", Scope: "user", Action: "read"}, Actor: sdk.Actor{ID: "1", Username: "soda-tester"}})
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(sdk.ContextHeader, string(authority))
	request.Header.Set(sdk.AdmissionHeader, "test-admission")
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || string(responseBody) != "sample:soda-tester" {
		t.Fatalf("HTTP response: status=%d body=%q err=%v", response.StatusCode, responseBody, err)
	}
	runtimeDir := m.running["sample"].runtimeDir
	m.running["sample"].client.Kill()
	deadline := time.Now().Add(3 * time.Second)
	for {
		m.mu.Lock()
		_, active := m.running["sample"]
		m.mu.Unlock()
		if !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("crashed extension remained registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(runtimeDir); !os.IsNotExist(err) {
		t.Fatalf("runtime dir retained after crash: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("manager could not restart after cleanup: %v", err)
	}
	waitStartupFinished(t, m)
	if _, _, ok := m.Lookup("sample"); !ok {
		t.Fatal("extension missing after manager restart")
	}
}

// A failed startup retains no partial state. Optional package failure
// is isolated and never fails startup, so the competing-route host
// configuration error is rejected before starting any backend.
func TestManagerStartFailureCleansUp(t *testing.T) {
	root := shortTestRoot(t)
	for _, id := range []string{"a-sample", "z-second"} {
		installTestPackage(t, root, id)
		manifest := fmt.Sprintf(`{"protocol":1,"id":%q,"name":"Test","version":"1.0","executable":"extension","pages":[{"id":"page","route":"workspace","title":"Workspace","scope":"global","entry":"main.js"}]}`, id)
		if err := os.WriteFile(filepath.Join(root, id, "extension.json"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		setDesiredState(t, root, id, true)
	}
	m := NewManager(root)
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	if err := m.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("expected competing-route startup failure, got %v", err)
	}
	if len(m.List()) != 0 {
		t.Fatal("partial startup remained registered")
	}
	runtimeDirs, err := os.ReadDir(filepath.Join(root, ".runtime"))
	if (err != nil && !os.IsNotExist(err)) || len(runtimeDirs) != 0 {
		t.Fatalf("runtime process directory remained: %+v, %v", runtimeDirs, err)
	}
	lock, err := packages.AcquirePackageLock(root)
	if err != nil {
		t.Fatalf("startup failure retained package lock: %v", err)
	}
	_ = lock.Close()
}

// An unreadable optional package never exposes its bytes in any
// diagnostic: the failure is isolated, core starts, and the observed
// failure text carries no manifest contents and no filesystem paths.
func TestStartupDiagnosticDoesNotExposeManifestContents(t *testing.T) {
	root := shortTestRoot(t)
	packageDir := filepath.Join(root, "sample")
	if err := os.Mkdir(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte("private-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	persistDesiredState(t, root, "sample", true)
	m := NewManager(root)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("optional package failure must not fail startup: %v", err)
	}
	waitStartupFinished(t, m)
	t.Cleanup(func() { _ = m.Close() })
	if _, _, ok := m.Lookup("sample"); ok {
		t.Fatal("broken extension must not serve admission")
	}
	observed := lifecycleResult(t, m.Observe("sample"))
	if observed.State != packages.LifecycleFailed || !observed.Unconfirmed {
		t.Fatalf("broken extension must read failed: %+v", observed)
	}
	message := ""
	if observed.Error != nil {
		message = observed.Error.Message
	}
	message += observed.Progress
	if strings.Contains(message, "private-token") || strings.Contains(message, root) {
		t.Fatalf("unsafe startup diagnostic: %q", message)
	}
}

func TestManagerRejectsUndeclaredRuntimeHandlers(t *testing.T) {
	for _, declarations := range []string{
		`"policies":["forgejo.username"]`,
		`"capabilities":["native.contribution.authorize"]`,
	} {
		root := shortTestRoot(t)
		installTestPackage(t, root, "sample")
		manifest := fmt.Sprintf(`{"protocol":1,"id":"sample","name":"Test","version":"1.0","executable":"extension",%s}`, declarations)
		if err := os.WriteFile(filepath.Join(root, "sample", "extension.json"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		setDesiredState(t, root, "sample", true)
		manager := NewManager(root)
		manager.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
		// Unmatched declarations never run: the optional failure is
		// isolated, core starts, and the package stays out of
		// admission with a failed observation.
		if err := manager.Start(context.Background()); err != nil {
			t.Fatalf("optional package failure must not fail startup: %v", err)
		}
		waitStartupFinished(t, manager)
		t.Cleanup(func() { _ = manager.Close() })
		if _, _, ok := manager.Lookup("sample"); ok {
			t.Fatalf("unmatched declaration %s serves admission", declarations)
		}
		observed := lifecycleResult(t, manager.Observe("sample"))
		if observed.State != packages.LifecycleFailed {
			t.Fatalf("unmatched declaration %s reads %+v", declarations, observed)
		}
	}
}

func TestCloneDescriptorCopiesDeclarations(t *testing.T) {
	original := Descriptor{Manifest: sdk.Manifest{Capabilities: []string{sdk.CapabilityActorRead}, Policies: []string{sdk.PolicyForgejoUsername}}}
	copy := cloneDescriptor(original)
	copy.Manifest.Capabilities[0] = sdk.CapabilityRepositoryRead
	copy.Manifest.Policies[0] = "changed"
	if original.Manifest.Capabilities[0] != sdk.CapabilityActorRead || original.Manifest.Policies[0] != sdk.PolicyForgejoUsername {
		t.Fatal("descriptor declarations share backing storage")
	}
}

func TestDisabledPackageSkipped(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	setDesiredState(t, root, "sample", false)
	m := NewManager(root)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitStartupFinished(t, m)
	defer m.Close()
	if len(m.List()) != 0 {
		t.Fatal("disabled extension started")
	}
}

func TestRequiredExtensionStartup(t *testing.T) {
	root := shortTestRoot(t)
	for _, ids := range [][]string{{""}, {"soda", "soda"}} {
		if err := NewManager(root, ids...).Start(context.Background()); err == nil {
			t.Fatalf("invalid required IDs accepted: %q", ids)
		}
	}
	manager := NewManager(root, "soda")
	sodaEntry := testCatalogEntry("soda", "1.0", true, "")
	configureTestCatalog(t, manager, root, sodaEntry)
	require.NoError(t, manager.SetCallbackHandlerFactory(func(string) http.Handler { return http.NotFoundHandler() }))
	if err := manager.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "bootstrap failed") {
		t.Fatalf("missing required package accepted: %v", err)
	}
	installCatalogPackage(t, root, sodaEntry, nil)
	persistDesiredState(t, root, "soda", false)
	if err := manager.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled required package accepted: %v", err)
	}
	if err := packages.RemoveIntent(root, "soda"); err != nil {
		t.Fatal(err)
	}
	// Required bootstrap reuses only exact installed receipts: the
	// fixture completes its install the way production installs do.
	installCatalogPackage(t, root, sodaEntry, nil)
	manager.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("enabled required package failed: %v", err)
	}
	waitStartupFinished(t, manager)
	defer manager.Close()
}

func TestRequiredPolicyFailsClosedAfterCrash(t *testing.T) {
	root := shortTestRoot(t)
	sodaEntry := testCatalogEntry("soda", "1.0", true, "")
	installCatalogPackage(t, root, sodaEntry, map[string]any{"policies": []string{"forgejo.username"}})
	manager := NewManager(root, "soda")
	configureTestCatalog(t, manager, root, sodaEntry)
	require.NoError(t, manager.SetCallbackHandlerFactory(func(string) http.Handler { return http.NotFoundHandler() }))
	manager.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1", "FORGEJO_EXTENSION_TEST_POLICY=allow"}
	var stopped string
	if err := manager.SetInstanceStopped(func(instanceID string) { stopped = instanceID }); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitStartupFinished(t, manager)
	defer manager.Close()
	decision, err := manager.EvaluateRequiredPolicy(context.Background(), sdk.PolicyForgejoUsername, sdk.PolicyRequest{Operation: "create", Username: "soda-tester"})
	if err != nil || !decision.Allowed {
		t.Fatalf("required policy did not allow: %+v, %v", decision, err)
	}
	if _, err := manager.EvaluateRequiredPolicy(context.Background(), "missing", sdk.PolicyRequest{}); err != ErrRequiredPolicyUnavailable {
		t.Fatalf("missing policy handler did not fail closed: %v", err)
	}
	instanceID := manager.running["soda"].descriptor.InstanceID
	manager.running["soda"].client.Kill()
	if _, err := manager.EvaluateRequiredPolicy(context.Background(), sdk.PolicyForgejoUsername, sdk.PolicyRequest{}); err != ErrRequiredPolicyUnavailable {
		t.Fatalf("crashed policy did not fail closed: %v", err)
	}
	if stopped != instanceID {
		t.Fatalf("stopped hook did not receive the dead instance: %q", stopped)
	}
}

func TestRequiredPolicyRuntimeDecision(t *testing.T) {
	previous := setting.Extensions
	setting.Extensions.Enabled = true
	setting.Extensions.RequiredIDs = []string{"soda"}
	t.Cleanup(func() { setting.Extensions = previous; SetDefault(nil) })
	for _, outcome := range []string{"allow", "deny", "timeout"} {
		t.Run(outcome, func(t *testing.T) {
			root := shortTestRoot(t)
			sodaEntry := testCatalogEntry("soda", "1.0", true, "")
			installCatalogPackage(t, root, sodaEntry, map[string]any{"policies": []string{"forgejo.username"}})
			manager := NewManager(root, "soda")
			configureTestCatalog(t, manager, root, sodaEntry)
			require.NoError(t, manager.SetCallbackHandlerFactory(func(string) http.Handler { return http.NotFoundHandler() }))
			manager.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1", "FORGEJO_EXTENSION_TEST_POLICY=" + outcome}
			if err := manager.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			waitStartupFinished(t, manager)
			defer manager.Close()
			SetDefault(manager)
			defer SetDefault(nil)
			started := time.Now()
			err := packages.CheckRequiredPolicy(context.Background(), sdk.PolicyForgejoUsername, sdk.PolicyRequest{Operation: "create", Username: "policy-tester"})
			switch outcome {
			case "allow":
				if err != nil {
					t.Fatal(err)
				}
			case "deny":
				if _, ok := err.(packages.ErrPolicyDenied); !ok {
					t.Fatalf("expected policy denial, got %v", err)
				}
			case "timeout":
				if err != ErrRequiredPolicyUnavailable {
					t.Fatalf("timeout did not fail closed: %v", err)
				}
				if elapsed := time.Since(started); elapsed > 5*time.Second {
					t.Fatalf("policy timeout took %v", elapsed)
				}
			}
		})
	}
}

func TestManagerRefusesCompetingNativeRoutes(t *testing.T) {
	root := shortTestRoot(t)
	for _, id := range []string{"first", "second"} {
		installTestPackage(t, root, id)
		manifest := fmt.Sprintf(`{"protocol":1,"id":%q,"name":"Test","version":"1","executable":"extension","pages":[{"id":"page","route":"workspace","title":"Workspace","scope":"global","entry":"main.js"}]}`, id)
		if err := os.WriteFile(filepath.Join(root, id, "extension.json"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		setDesiredState(t, root, id, true)
	}
	m := NewManager(root)
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	if err := m.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("competing public route accepted: %v", err)
	}
	if len(m.List()) != 0 {
		t.Fatal("failed startup retained partial route registry")
	}
}

// A deliberately slow optional handshake proves core readiness is independent
// of the optional process, while shutdown still joins its eventual cleanup.
func TestOptionalHandshakeDoesNotBlockCoreReadiness(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	intent := setDesiredState(t, root, "sample", true)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".data", "sample"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".data", "sample", "fixture.json"), []byte(`{"slow_start_ms":1500}`), 0o600))
	m := NewManager(root)
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	t.Cleanup(func() { _ = m.Close() })
	started := time.Now()
	require.NoError(t, m.Start(context.Background()))
	require.Less(t, time.Since(started), time.Second)
	_, _, ready := m.Lookup("sample")
	require.False(t, ready, "slow optional handshake must remain closed")
	waitStartupFinished(t, m)
	_, _, ready = m.Lookup("sample")
	require.True(t, ready)
	after, err := packages.LoadIntent(root, "sample")
	require.NoError(t, err)
	require.Equal(t, intent.Generation, after.Generation)
}
