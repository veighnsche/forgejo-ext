// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

//go:build !linux

package callback

import (
	"errors"
	"net"
)

// Credential is unavailable without SO_PEERCRED; Unix service callback
// peers cannot be authenticated on this platform.
func Credential(_ net.Conn) (Peer, error) {
	return Peer{}, errors.New("unix peer credentials are unavailable")
}
