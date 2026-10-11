// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
)

// LifecycleIssuePayload is the issue.lifecycle.apply intent payload: at
// most one label and one assignee on one exact issue. The host validates
// authoritatively; the SDK validation below only catches submitter bugs
// early.
type LifecycleIssuePayload struct {
	IssueID    int64 `json:"issue_id"`
	LabelID    int64 `json:"label_id,omitempty"`
	AssigneeID int64 `json:"assignee_id,omitempty"`
}

// ErrInvalidLifecycleIssuePayload reports a malformed lifecycle payload.
var ErrInvalidLifecycleIssuePayload = errors.New("invalid lifecycle issue payload")

// ValidateLifecycleIssuePayload checks the structural intent rules: a
// positive issue ID and at least one nonnegative label/assignee effect.
func ValidateLifecycleIssuePayload(p LifecycleIssuePayload) error {
	if p.IssueID <= 0 || p.LabelID < 0 || p.AssigneeID < 0 {
		return ErrInvalidLifecycleIssuePayload
	}
	if p.LabelID == 0 && p.AssigneeID == 0 {
		return ErrInvalidLifecycleIssuePayload
	}
	return nil
}
