// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package callback

import (
	"errors"
	"net"

	"golang.org/x/sys/unix"
)

// Credential returns the kernel Unix peer credentials for a connected Unix
// socket. UIDs are interpreted in the caller's user namespace.
func Credential(conn net.Conn) (Peer, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return Peer{}, errors.New("unix peer credentials require a unix connection")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return Peer{}, errors.New("unix peer credentials are unavailable")
	}
	var peer Peer
	var syscallErr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			syscallErr = err
			return
		}
		peer = Peer{UID: cred.Uid, GID: cred.Gid, PID: cred.Pid}
	}); err != nil {
		return Peer{}, errors.New("unix peer credentials are unavailable")
	}
	if syscallErr != nil {
		return Peer{}, errors.New("unix peer credentials are unavailable")
	}
	if peer.PID <= 0 {
		return Peer{}, errors.New("unix peer credentials are unavailable")
	}
	return peer, nil
}
