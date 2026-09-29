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
	"testing"
	"time"

	"forgejo.org/modules/extensions"
)

func TestMain(m *testing.M) {
	if os.Getenv("FORGEJO_EXTENSION_TEST_HELPER") == "1" {
		err := extensions.Serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authority, err := extensions.RequestContext(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, authority.ExtensionID+":"+authority.Actor.Username)
		}))
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
	if lock, err := extensions.AcquirePackageLock(root); err == nil {
		_ = lock.Close()
		t.Fatal("package mutation lock was available while extension was running")
	}
	if _, err := extensions.Install(root, filepath.Join(root, "sample"), true); err == nil {
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
	authority, err := json.Marshal(extensions.RequestAuthority{ExtensionID: "sample", Scope: "user", Actor: extensions.Actor{ID: 1, Username: "soda-tester"}})
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(extensions.ContextHeader, string(authority))
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
		_, _, ok := m.Lookup("sample")
		if !ok {
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
	lock, err := extensions.AcquirePackageLock(root)
	if err != nil {
		t.Fatalf("startup failure retained package lock: %v", err)
	}
	_ = lock.Close()
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
