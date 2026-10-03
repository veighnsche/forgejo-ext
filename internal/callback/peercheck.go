// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package callback verifies Unix-socket peers for the extension service
// callback channel. It is stdlib plus x/sys only: the dial side proves
// reachability and host authority before sending anything, and the accept
// side binds each connection to an explicitly configured peer mapping.
// Socket group membership or manifest declaration alone never authorizes a
// peer, and an empty mapping rejects every peer.
package callback

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"
)

// Peer identifies a Unix socket peer from kernel credentials.
type Peer struct {
	UID uint32
	GID uint32
	PID int32
}

// Format encodes a peer in the canonical RemoteAddr form shared with the
// extension SDK: uid=N;gid=N;pid=N.
func Format(peer Peer) string {
	return "uid=" + strconv.FormatUint(uint64(peer.UID), 10) +
		";gid=" + strconv.FormatUint(uint64(peer.GID), 10) +
		";pid=" + strconv.FormatInt(int64(peer.PID), 10)
}

// Parse decodes a canonical peer address. Anything else, including a
// missing or non-positive PID, is rejected: handlers must treat an
// unparseable address as an unauthenticated peer.
func Parse(raw string) (Peer, error) {
	var peer Peer
	parts := strings.Split(raw, ";")
	if len(parts) != 3 {
		return peer, errors.New("invalid unix peer address")
	}
	for _, part := range parts {
		key, value, ok := strings.Cut(part, "=")
		if !ok || value == "" {
			return peer, errors.New("invalid unix peer address")
		}
		switch key {
		case "uid", "gid":
			number, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return peer, errors.New("invalid unix peer address")
			}
			if key == "uid" {
				peer.UID = uint32(number)
			} else {
				peer.GID = uint32(number)
			}
		case "pid":
			number, err := strconv.ParseInt(value, 10, 32)
			if err != nil || number <= 0 {
				return peer, errors.New("invalid unix peer address")
			}
			peer.PID = int32(number)
		default:
			return peer, errors.New("invalid unix peer address")
		}
	}
	return peer, nil
}

// Authorize maps an observed peer UID to exactly one configured package.
// A missing or empty mapping rejects every peer, including UID 0: only an
// explicit operator entry authorizes a service peer.
func Authorize(peer Peer, allowed map[uint32]string) (string, error) {
	if len(allowed) == 0 {
		return "", errors.New("service callback peer is not permitted")
	}
	packageID, ok := allowed[peer.UID]
	if !ok || packageID == "" {
		return "", errors.New("service callback peer is not permitted")
	}
	return packageID, nil
}

func checkSocketPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid service callback socket path")
	}
	return nil
}

// DialVerified dials a Unix service socket and verifies the listener's
// kernel peer UID before returning the connection. A UID mismatch or
// unreadable credentials closes the connection and reports an error, so
// callers never send a bootstrap over an unauthenticated channel.
func DialVerified(ctx context.Context, socketPath string, expectedUID uint32) (net.Conn, Peer, error) {
	if err := checkSocketPath(socketPath); err != nil {
		return nil, Peer{}, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, Peer{}, err
	}
	peer, err := Credential(conn)
	if err != nil {
		_ = conn.Close()
		return nil, Peer{}, err
	}
	if peer.UID != expectedUID {
		_ = conn.Close()
		return nil, Peer{}, errors.New("service callback host peer is not permitted")
	}
	return conn, peer, nil
}

// VerifyConnection binds an accepted Unix connection to the configured peer
// mapping. Connections without kernel credentials are rejected.
func VerifyConnection(conn net.Conn, allowed map[uint32]string) (string, error) {
	peer, err := Credential(conn)
	if err != nil {
		return "", err
	}
	return Authorize(peer, allowed)
}
