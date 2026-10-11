// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"errors"
	"strings"
)

// OperationsHandshake is the read-only native operations answer: the
// host-derived installation, a stable operations generation, and the
// supported operation kinds. Callers must verify installation against
// their pinned admission and treat an empty kind list as no support.
type OperationsHandshake struct {
	Installation string   `json:"installation"`
	Generation   string   `json:"generation"`
	Kinds        []string `json:"kinds"`
}

// ErrInvalidOperationsHandshake reports an unattributable handshake.
var ErrInvalidOperationsHandshake = errors.New("invalid operations handshake")

// ValidateOperationsHandshake checks the structural handshake rules:
// nonempty installation and generation, and well-formed kind strings.
func ValidateOperationsHandshake(h OperationsHandshake) error {
	if h.Installation == "" || h.Generation == "" || len(h.Installation) > 128 || len(h.Generation) > 128 {
		return ErrInvalidOperationsHandshake
	}
	if len(h.Kinds) > 64 {
		return ErrInvalidOperationsHandshake
	}
	for _, kind := range h.Kinds {
		if kind == "" || len(kind) > 128 || strings.ContainsAny(kind, " \t\n\r") {
			return ErrInvalidOperationsHandshake
		}
	}
	return nil
}

// ReadOperations queries the read-only operations handshake. The answer
// installation must equal the client's pinned installation.
func (c *backgroundClient) ReadOperations(ctx context.Context) (OperationsHandshake, error) {
	var handshake OperationsHandshake
	if err := c.post(ctx, BackgroundOperationsPath, struct{}{}, &handshake); err != nil {
		return OperationsHandshake{}, err
	}
	if err := ValidateOperationsHandshake(handshake); err != nil {
		return OperationsHandshake{}, err
	}
	if handshake.Installation != c.installation {
		return OperationsHandshake{}, errors.New("invalid background response")
	}
	return handshake, nil
}
