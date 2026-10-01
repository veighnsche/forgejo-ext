// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCredentialFile(t *testing.T, perm os.FileMode) CredentialFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("faketokenvalue0123456789abcdef01234567\n"), perm); err != nil {
		t.Fatal(err)
	}
	return CredentialFile(path)
}

func TestCredentialFileRejectsUnsafeInput(t *testing.T) {
	if _, err := CredentialFile("").read(); err == nil {
		t.Fatal("empty credential path must fail")
	}
	if _, err := CredentialFile(filepath.Join(t.TempDir(), "missing")).read(); err == nil {
		t.Fatal("missing credential file must fail")
	}
	if _, err := writeCredentialFile(t, 0o604).read(); err == nil {
		t.Fatal("other-readable credential file must fail")
	}
	secret, err := writeCredentialFile(t, 0o600).read()
	if err != nil {
		t.Fatal(err)
	}
	if secret != "faketokenvalue0123456789abcdef01234567" {
		t.Fatalf("credential secret was mangled: %q", secret)
	}
}

func TestUnixPeerAddressRoundTrip(t *testing.T) {
	peer, err := ParseUnixPeer(FormatUnixPeer(UnixPeer{UID: 1001, GID: 1002, PID: 4242}))
	if err != nil {
		t.Fatal(err)
	}
	if peer.UID != 1001 || peer.GID != 1002 || peer.PID != 4242 {
		t.Fatalf("peer round trip failed: %+v", peer)
	}
	for _, invalid := range []string{"", "uid=1;gid=2", "uid=x;gid=2;pid=3", "uid=1;gid=2;pid=0", "uid=1;gid=2;pid=3;extra=4"} {
		if _, err := ParseUnixPeer(invalid); err == nil {
			t.Fatalf("invalid peer address %q must fail", invalid)
		}
	}
}

func TestRuntimeBackgroundClientRequiresDelivery(t *testing.T) {
	storeBackgroundDelivery(BackgroundAdmissionDelivery{})
	t.Cleanup(func() { storeBackgroundDelivery(BackgroundAdmissionDelivery{}) })
	if _, err := RuntimeBackgroundClient(); err == nil {
		t.Fatal("missing delivery must fail")
	}
}

func unixBackgroundServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "background.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	server.Start()
	t.Cleanup(server.Close)
	return socket
}

func TestBackgroundClientRoundTrip(t *testing.T) {
	const admission = "0123456789abcdefghij0123456789abcdefghijk"
	socket := unixBackgroundServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(AdmissionHeader) != admission {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case BackgroundSubmitPath:
			var request struct {
				OperationIntent
				Token string `json:"token"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if request.Token == "" || request.Kind != OperationKindMerge {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(OperationRecord{InstallationID: "install-1", OperationID: request.OperationID, Outcome: BackgroundOutcomeOperationsUnavailable})
		case BackgroundGetPath:
			_ = json.NewEncoder(w).Encode(OperationLookup{InstallationID: "install-1", OperationID: "op-1", Status: BackgroundOutcomeNotObserved})
		default:
			http.NotFound(w, r)
		}
	}))
	client := &backgroundClient{socket: socket, admission: admission, installation: "install-1"}
	ctx := context.Background()

	record, err := client.SubmitOperation(ctx, writeCredentialFile(t, 0o600), OperationIntent{
		OperationID: "op-1", ActorID: "42", RepositoryID: "7",
		Kind: OperationKindMerge, AuthorizationRevision: "rev-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Outcome != BackgroundOutcomeOperationsUnavailable || record.InstallationID != "install-1" {
		t.Fatalf("unexpected submit record: %+v", record)
	}
	if _, err := client.SubmitOperation(ctx, writeCredentialFile(t, 0o600), OperationIntent{
		OperationID: "op-2", InstallationID: "other-install", ActorID: "42",
		RepositoryID: "7", Kind: OperationKindMerge, AuthorizationRevision: "rev-1",
	}); err == nil {
		t.Fatal("conflicting installation must fail client-side")
	}
	lookup, err := client.GetOperation(ctx, "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if lookup.Status != BackgroundOutcomeNotObserved {
		t.Fatalf("unexpected lookup: %+v", lookup)
	}
	if _, err := client.GetOperation(ctx, "bad id!"); err == nil {
		t.Fatal("invalid operation id must fail")
	}
}

func TestBackgroundClientRejectsForeignInstallation(t *testing.T) {
	const admission = "0123456789abcdefghij0123456789abcdefghijk"
	socket := unixBackgroundServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OperationLookup{InstallationID: "foreign", OperationID: "op-1", Status: BackgroundOutcomeNotObserved})
	}))
	client := &backgroundClient{socket: socket, admission: admission, installation: "install-1"}
	if _, err := client.GetOperation(context.Background(), "op-1"); err == nil {
		t.Fatal("foreign installation response must fail")
	}
}

func TestBootstrapRejectsUnexpectedHostPeer(t *testing.T) {
	socket := unixBackgroundServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(struct {
			Admission      string `json:"admission"`
			InstallationID string `json:"installation_id"`
		}{Admission: strings.Repeat("a", 43), InstallationID: "install-1"})
	}))
	_, err := BootstrapServiceBackground(context.Background(), ServiceBridgeOptions{
		SocketPath:      socket,
		ExpectedHostUID: 42424242,
	})
	if err == nil {
		t.Fatal("unexpected host UID must fail bootstrap")
	}
}
