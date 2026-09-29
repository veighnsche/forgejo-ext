// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"
	"forgejo.org/modules/setting"
)

func TestMain(m *testing.M) {
	if os.Getenv("FORGEJO_EXTENSION_TEST_HELPER") == "1" {
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
		err := sdk.Serve(application)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
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

func shortTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(os.TempDir(), "e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func TestManagerNativeHTTPAndCrashCleanup(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "sample")
	m := NewManager(root)
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if lock, err := packages.AcquirePackageLock(root); err == nil {
		_ = lock.Close()
		t.Fatal("package mutation lock was available while extension was running")
	}
	if _, err := packages.Install(root, filepath.Join(root, "sample"), true); err == nil {
		t.Fatal("package install succeeded while extension was running")
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
	if _, _, ok := m.Lookup("sample"); !ok {
		t.Fatal("extension missing after manager restart")
	}
}

func TestManagerStartFailureCleansUp(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "a-sample")
	if err := os.Mkdir(filepath.Join(root, "z-broken"), 0o700); err != nil {
		t.Fatal(err)
	}
	m := NewManager(root)
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	if err := m.Start(context.Background()); err == nil {
		t.Fatal("expected startup failure")
	}
	if len(m.List()) != 0 {
		t.Fatal("partial startup remained registered")
	}
	runtimeDirs, err := os.ReadDir(filepath.Join(root, ".runtime"))
	if err != nil || len(runtimeDirs) != 0 {
		t.Fatalf("runtime process directory remained: %+v, %v", runtimeDirs, err)
	}
	lock, err := packages.AcquirePackageLock(root)
	if err != nil {
		t.Fatalf("startup failure retained package lock: %v", err)
	}
	_ = lock.Close()
}

func TestStartupDiagnosticDoesNotExposeManifestContents(t *testing.T) {
	root := shortTestRoot(t)
	packageDir := filepath.Join(root, "sample")
	if err := os.Mkdir(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte("private-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := NewManager(root).Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "package validation failed") || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), root) {
		t.Fatalf("unsafe startup diagnostic: %v", err)
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
		manager := NewManager(root)
		manager.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
		if err := manager.Start(context.Background()); err == nil {
			t.Fatalf("accepted unmatched declaration %s", declarations)
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
	if err := os.WriteFile(filepath.Join(root, "sample", ".disabled"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(root)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
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
	if err := manager.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing required package accepted: %v", err)
	}
	installTestPackage(t, root, "soda")
	if err := os.WriteFile(filepath.Join(root, "soda", ".disabled"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled required package accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "soda", ".disabled")); err != nil {
		t.Fatal(err)
	}
	manager.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("enabled required package failed: %v", err)
	}
	defer manager.Close()
}

func TestRequiredPolicyFailsClosedAfterCrash(t *testing.T) {
	root := shortTestRoot(t)
	installTestPackage(t, root, "soda")
	manifest := `{"protocol":1,"id":"soda","name":"Test","version":"1.0","executable":"extension","policies":["forgejo.username"]}`
	if err := os.WriteFile(filepath.Join(root, "soda", "extension.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(root, "soda")
	manager.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1", "FORGEJO_EXTENSION_TEST_POLICY=allow"}
	var stopped string
	if err := manager.SetInstanceStopped(func(instanceID string) { stopped = instanceID }); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
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
			installTestPackage(t, root, "soda")
			manifest := `{"protocol":1,"id":"soda","name":"Test","version":"1.0","executable":"extension","policies":["forgejo.username"]}`
			if err := os.WriteFile(filepath.Join(root, "soda", "extension.json"), []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			manager := NewManager(root, "soda")
			manager.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1", "FORGEJO_EXTENSION_TEST_POLICY=" + outcome}
			if err := manager.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
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
