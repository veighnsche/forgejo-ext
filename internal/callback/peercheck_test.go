// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package callback

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseRoundTrip(t *testing.T) {
	peer, err := Parse("uid=1000;gid=100;pid=123")
	if err != nil {
		t.Fatal(err)
	}
	if peer.UID != 1000 || peer.GID != 100 || peer.PID != 123 {
		t.Fatalf("unexpected peer: %+v", peer)
	}
	if formatted := Format(peer); formatted != "uid=1000;gid=100;pid=123" {
		t.Fatalf("unexpected format: %q", formatted)
	}
	back, err := Parse(Format(peer))
	if err != nil || back != peer {
		t.Fatalf("round trip failed: %+v, %v", back, err)
	}
}

func TestParseRejects(t *testing.T) {
	for _, raw := range []string{
		"",
		"uid=1000",
		"uid=1000;gid=100",
		"uid=;gid=100;pid=123",
		"uid=x;gid=100;pid=123",
		"uid=1000;gid=100;pid=0",
		"uid=1000;gid=100;pid=-5",
		"uid=1000;gid=100;pid=123;extra=1",
		"user=1000;gid=100;pid=123",
		"uid=4294967296;gid=100;pid=123",
		"@",
	} {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("Parse accepted %q", raw)
		}
	}
}

func TestAuthorizeRequiresExplicitMapping(t *testing.T) {
	peer := Peer{UID: 1000, GID: 100, PID: 123}
	if _, err := Authorize(peer, nil); err == nil {
		t.Fatal("nil mapping authorized a peer")
	}
	if _, err := Authorize(peer, map[uint32]string{}); err == nil {
		t.Fatal("empty mapping authorized a peer")
	}
	if _, err := Authorize(peer, map[uint32]string{1001: "other"}); err == nil {
		t.Fatal("unmapped UID authorized a peer")
	}
	if _, err := Authorize(peer, map[uint32]string{1000: ""}); err == nil {
		t.Fatal("empty package authorized a peer")
	}
	packageID, err := Authorize(peer, map[uint32]string{1000: "soda-service"})
	if err != nil || packageID != "soda-service" {
		t.Fatalf("mapped peer rejected: %q, %v", packageID, err)
	}
}

// TestDialVerifiedOverRealSocket proves reachability and authority over a
// disposable Unix socket with kernel credentials: the dialer observes the
// listener's real UID/PID, a wrong expected UID is refused before any
// bootstrap bytes flow, and the accept side binds to the same mapping.
func TestDialVerifiedOverRealSocket(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("SO_PEERCRED proof requires linux")
	}
	path := filepath.Join(t.TempDir(), "cb.sock")
	if len(path) >= 107 {
		t.Fatalf("socket path too long for the fixture: %q", path)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	self := uint32(os.Geteuid())
	conn, peer, err := DialVerified(context.Background(), path, self)
	if err != nil {
		t.Fatalf("verified dial failed: %v", err)
	}
	defer conn.Close()
	if peer.UID != self {
		t.Fatalf("observed UID %d, want %d", peer.UID, self)
	}
	if peer.PID != int32(os.Getpid()) {
		t.Fatalf("observed PID %d, want %d", peer.PID, os.Getpid())
	}

	var serverConn net.Conn
	select {
	case serverConn = <-accepted:
	case err := <-acceptErr:
		t.Fatalf("accept failed: %v", err)
	}
	defer serverConn.Close()
	packageID, err := VerifyConnection(serverConn, map[uint32]string{self: "soda-service"})
	if err != nil || packageID != "soda-service" {
		t.Fatalf("accept side rejected the mapped peer: %q, %v", packageID, err)
	}
	if _, err := VerifyConnection(serverConn, map[uint32]string{self + 1: "other"}); err == nil {
		t.Fatal("accept side authorized an unmapped peer")
	}

	if _, _, err := DialVerified(context.Background(), path, self+1); err == nil {
		t.Fatal("dial with the wrong expected UID succeeded")
	}
	if _, _, err := DialVerified(context.Background(), filepath.Join(t.TempDir(), "missing.sock"), self); err == nil {
		t.Fatal("dial to a missing socket succeeded")
	}
	if _, _, err := DialVerified(context.Background(), "relative.sock", self); err == nil {
		t.Fatal("dial to a relative socket path succeeded")
	}
}
