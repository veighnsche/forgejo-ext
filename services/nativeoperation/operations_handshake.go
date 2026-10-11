// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	model "forgejo.org/models/nativeoperation"
)

// OperationsHandshake describes the host's supported background
// operations: a stable generation digest plus the sorted kind list. The
// generation changes if and only if the supported kind set changes;
// native replacement is additionally caught by installation identity and
// admission revocation. This query is read-only: no reservation, no
// mutation, no capability file.
type OperationsHandshake struct {
	Generation string
	Kinds      []string
}

// SupportedOperations returns the current handshake facts. The context
// is accepted for the router seam; the query performs no I/O.
func (s *Service) SupportedOperations(_ context.Context) (OperationsHandshake, error) {
	kinds := model.SupportedKinds()
	sum := sha256.Sum256([]byte(strings.Join(kinds, "\n")))
	return OperationsHandshake{
		Generation: "ops-" + hex.EncodeToString(sum[:])[:16],
		Kinds:      kinds,
	}, nil
}
