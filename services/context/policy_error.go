// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package context

import (
	"errors"
	"net/http"
	"strings"

	nativeoperation "forgejo.org/models/nativeoperation"
	"forgejo.org/modules/extensions"
)

// policyErrorStatus preserves a policy veto versus a required runtime outage at
// the shared web/API error boundary, including wrapped external-auth errors.
// Native-mutation contention maps to 503 so every participating writer
// reports busy uniformly without per-router handling. It matches the
// models-level reservation errors; the service package cannot be imported
// here (import cycle through the pull service), so the service-level
// reservation sentinel — which shares the models sentinel's message and
// documents the same HTTP 503 intent — is matched by its text.
func policyErrorStatus(err error) int {
	if errors.Is(err, nativeoperation.ErrBusy) || errors.Is(err, nativeoperation.ErrInhibited) {
		return http.StatusServiceUnavailable
	}
	if err != nil && strings.Contains(err.Error(), nativeoperation.ErrBusy.Error()) {
		return http.StatusServiceUnavailable
	}
	if errors.Is(err, extensions.ErrRequiredPolicyUnavailable) {
		return http.StatusServiceUnavailable
	}
	var denied extensions.ErrPolicyDenied
	if errors.As(err, &denied) {
		return http.StatusUnprocessableEntity
	}
	return 0
}

// HandlePolicyError renders only typed policy errors; callers retain all their
// existing handling for native validation and unrelated failures.
func (ctx *Context) HandlePolicyError(err error) bool {
	if status := policyErrorStatus(err); status != 0 {
		ctx.PlainText(status, err.Error())
		return true
	}
	return false
}
