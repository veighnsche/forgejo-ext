// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"strings"
)

// MergeMethodFastForwardOnly is the only supported merge method. There is no
// fallback method and no parameter selecting another engine.
const MergeMethodFastForwardOnly = "fast-forward-only"

// ErrInvalidMergePayload reports a malformed merge payload.
var ErrInvalidMergePayload = errors.New("invalid merge payload")

func validMergePayloadOID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	for _, c := range oid {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
			continue
		}
		return false
	}
	return true
}

func validMergePayloadRef(ref string) bool {
	if !strings.HasPrefix(ref, "refs/heads/") || len(ref) > 512 {
		return false
	}
	name := strings.TrimSpace(ref)
	if name != ref || strings.ContainsAny(ref, " ~^:?*\\") || strings.Contains(ref, "..") {
		return false
	}
	return len(strings.TrimPrefix(ref, "refs/heads/")) > 0
}

// ValidateMergePayload checks the structural merge intent rules: a positive
// PR number, a same-repository head, distinct full branch refs, distinct
// valid exact OIDs and the fast-forward-only method. The host re-validates
// everything against live native state.
func ValidateMergePayload(repositoryID int64, payload MergePayload) error {
	if payload.PullRequestNumber <= 0 {
		return ErrInvalidMergePayload
	}
	if payload.HeadRepositoryID != repositoryID {
		return ErrInvalidMergePayload
	}
	if !validMergePayloadRef(payload.HeadRef) || !validMergePayloadRef(payload.BaseRef) {
		return ErrInvalidMergePayload
	}
	if payload.HeadRef == payload.BaseRef {
		return ErrInvalidMergePayload
	}
	if !validMergePayloadOID(payload.ExpectedHeadOID) || !validMergePayloadOID(payload.ExpectedBaseOID) {
		return ErrInvalidMergePayload
	}
	if strings.EqualFold(payload.ExpectedHeadOID, payload.ExpectedBaseOID) {
		return ErrInvalidMergePayload
	}
	if payload.Method != MergeMethodFastForwardOnly {
		return ErrInvalidMergePayload
	}
	return nil
}
