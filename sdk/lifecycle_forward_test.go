// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// forwardEchoFixture serves one exact echoed observation for the admitted
// call, checking the admission header and typed body like a dedicated Go
// private endpoint would before its own authorize callback.
type forwardEchoFixture struct {
	t        *testing.T
	token    string
	admitted LifecycleCall
	status   int
	body     []byte
}

func (f *forwardEchoFixture) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusBadRequest)
			return
		}
		if r.Header.Get(LifecycleAdmissionHeader) != f.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if f.body != nil {
			w.WriteHeader(f.status)
			_, _ = w.Write(f.body)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "malformed", http.StatusBadRequest)
			return
		}
		call, err := DecodeLifecycleCall(body)
		if err != nil {
			http.Error(w, "malformed", http.StatusBadRequest)
			return
		}
		if call != f.admitted {
			http.Error(w, "mismatched", http.StatusForbidden)
			return
		}
		writeJSON(w, LifecycleObservation{
			LifecycleCall:  call,
			State:          LifecycleStateStopped,
			AdmissionReady: false,
			Detail:         "forwarded",
		})
	}
}

func serveForwardFixture(t *testing.T, fixture *forwardEchoFixture) string {
	t.Helper()
	// Unix socket paths are capped near 108 bytes: keep the leaf prefix
	// short and honor the runner TMPDIR (os.MkdirTemp("", ...) uses it).
	dir, err := os.MkdirTemp("", "fx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "f.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: fixture.handler()}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return socket
}

// ForwardUnix validates against the real clock, unlike the frozen validation fixtures.
func liveForwardCall() LifecycleCall {
	call := frozenCall()
	call.DeadlineUnix = time.Now().Add(time.Minute).Unix()
	return call
}

func TestForwardUnixEchoesExactCall(t *testing.T) {
	call := liveForwardCall()
	fixture := &forwardEchoFixture{t: t, token: "forward-token", admitted: call}
	socket := serveForwardFixture(t, fixture)
	authority := LifecycleAuthority{admission: "forward-token"}
	observation, err := authority.ForwardUnix(context.Background(), socket, uint32(os.Geteuid()), "/forward", call)
	if err != nil {
		t.Fatalf("exact forward rejected: %v", err)
	}
	if observation.LifecycleCall != call || observation.State != LifecycleStateStopped || observation.AdmissionReady {
		t.Fatalf("forward echo wrong: %+v", observation)
	}
}

func TestForwardUnixRejectsBadAuthorityAndPeer(t *testing.T) {
	call := liveForwardCall()
	fixture := &forwardEchoFixture{t: t, token: "forward-token", admitted: call}
	socket := serveForwardFixture(t, fixture)
	if _, err := (LifecycleAuthority{}).ForwardUnix(context.Background(), socket, uint32(os.Geteuid()), "/forward", call); err == nil {
		t.Fatal("zero authority forwarded")
	}
	authority := LifecycleAuthority{admission: "forward-token"}
	if _, err := authority.ForwardUnix(context.Background(), socket, uint32(os.Geteuid())+1, "/forward", call); err == nil {
		t.Fatal("wrong peer UID forwarded")
	}
}

func TestForwardUnixRejectsBadTargetsAndShapes(t *testing.T) {
	call := liveForwardCall()
	fixture := &forwardEchoFixture{t: t, token: "forward-token", admitted: call}
	socket := serveForwardFixture(t, fixture)
	authority := LifecycleAuthority{admission: "forward-token"}
	for _, target := range [][2]string{
		{"relative.sock", "/forward"},
		{socket, "forward"},
		{socket, "/forward?x=1"},
		{socket, "/forward#frag"},
	} {
		if _, err := authority.ForwardUnix(context.Background(), target[0], uint32(os.Geteuid()), target[1], call); err == nil {
			t.Fatalf("bad target forwarded: %q", target)
		}
	}
	expired := call
	expired.DeadlineUnix = time.Now().Add(-time.Hour).Unix()
	if _, err := authority.ForwardUnix(context.Background(), socket, uint32(os.Geteuid()), "/forward", expired); err == nil {
		t.Fatal("expired call forwarded")
	}
	unknownOp := call
	unknownOp.Operation = "reboot"
	if _, err := authority.ForwardUnix(context.Background(), socket, uint32(os.Geteuid()), "/forward", unknownOp); err == nil {
		t.Fatal("unknown operation forwarded")
	}
}

func TestForwardUnixRefusesRedirectWithoutSecondHit(t *testing.T) {
	call := liveForwardCall()
	var followups atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/forward", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(LifecycleAdmissionHeader) != "forward-token" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Location", "/forward-followup")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/forward-followup", func(w http.ResponseWriter, r *http.Request) {
		followups.Add(1)
		writeJSON(w, LifecycleObservation{LifecycleCall: call, State: "enabled", AdmissionReady: true})
	})
	socket := filepath.Join(t.TempDir(), "redirect.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	authority := LifecycleAuthority{admission: "forward-token"}
	if _, err := authority.ForwardUnix(context.Background(), socket, uint32(os.Geteuid()), "/forward", call); err == nil {
		t.Fatal("redirected forward succeeded")
	}
	if followups.Load() != 0 {
		t.Fatalf("redirect was followed %d times", followups.Load())
	}
}

func TestForwardUnixRejectsSemanticallyInvalidObservation(t *testing.T) {
	call := liveForwardCall()
	// Decodes strictly but violates grant semantics: admission ready on a
	// non-running observation must fail even with a 200 and full echo.
	bad, err := json.Marshal(LifecycleObservation{
		LifecycleCall:  call,
		State:          "enabled",
		AdmissionReady: true,
		Detail:         "semantic",
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &forwardEchoFixture{t: t, status: http.StatusOK, body: bad}
	socket := serveForwardFixture(t, fixture)
	authority := LifecycleAuthority{admission: "forward-token"}
	if _, err := authority.ForwardUnix(context.Background(), socket, uint32(os.Geteuid()), "/forward", call); err == nil {
		t.Fatal("semantically invalid observation accepted")
	}
}

func TestForwardUnixFailsClosedOnTransport(t *testing.T) {
	call := liveForwardCall()
	authority := LifecycleAuthority{admission: "forward-token"}
	// Cancelled context fails without sending.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	fixture := &forwardEchoFixture{t: t, token: "forward-token", admitted: call}
	socket := serveForwardFixture(t, fixture)
	if _, err := authority.ForwardUnix(cancelled, socket, uint32(os.Geteuid()), "/forward", call); err == nil {
		t.Fatal("cancelled forward succeeded")
	}
	// Non-200, oversize and mismatched echo all fail closed.
	denied := &forwardEchoFixture{t: t, token: "other-token", admitted: call}
	deniedSocket := serveForwardFixture(t, denied)
	if _, err := authority.ForwardUnix(context.Background(), deniedSocket, uint32(os.Geteuid()), "/forward", call); err == nil {
		t.Fatal("forbidden forward succeeded")
	}
	big := &forwardEchoFixture{t: t, status: http.StatusOK, body: []byte(`"` + strings.Repeat("x", MaximumLifecycleBodyBytes+8) + `"`)}
	bigSocket := serveForwardFixture(t, big)
	if _, err := authority.ForwardUnix(context.Background(), bigSocket, uint32(os.Geteuid()), "/forward", call); err == nil {
		t.Fatal("oversize forward succeeded")
	}
	wrong := &forwardEchoFixture{t: t, token: "forward-token", admitted: LifecycleCall{}}
	wrongSocket := serveForwardFixture(t, wrong)
	if _, err := authority.ForwardUnix(context.Background(), wrongSocket, uint32(os.Geteuid()), "/forward", call); err == nil {
		t.Fatal("mismatched echo accepted")
	}
}
