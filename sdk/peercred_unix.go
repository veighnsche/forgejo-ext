// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"net"

	"golang.org/x/sys/unix"
)

// PeerCredential returns the kernel Unix peer credentials for a connected
// Unix socket. Both background peers use it to verify the configured native
// identity; UIDs are interpreted in the caller's user namespace.
func PeerCredential(conn net.Conn) (UnixPeer, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return UnixPeer{}, errors.New("unix peer credentials require a unix connection")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return UnixPeer{}, errors.New("unix peer credentials are unavailable")
	}
	var peer UnixPeer
	var syscallErr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			syscallErr = err
			return
		}
		peer = UnixPeer{UID: cred.Uid, GID: cred.Gid, PID: cred.Pid}
	}); err != nil {
		return UnixPeer{}, errors.New("unix peer credentials are unavailable")
	}
	if syscallErr != nil {
		return UnixPeer{}, errors.New("unix peer credentials are unavailable")
	}
	if peer.PID <= 0 {
		return UnixPeer{}, errors.New("unix peer credentials are unavailable")
	}
	return peer, nil
}
