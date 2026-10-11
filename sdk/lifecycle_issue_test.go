// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"testing"
)

func TestValidateLifecycleIssuePayload(t *testing.T) {
	if err := ValidateLifecycleIssuePayload(LifecycleIssuePayload{IssueID: 42, LabelID: 9}); err != nil {
		t.Fatalf("label-only rejected: %v", err)
	}
	if err := ValidateLifecycleIssuePayload(LifecycleIssuePayload{IssueID: 42, AssigneeID: 15}); err != nil {
		t.Fatalf("assignee-only rejected: %v", err)
	}
	for i, p := range []LifecycleIssuePayload{{}, {IssueID: 42}, {IssueID: 0, LabelID: 9}, {IssueID: 42, LabelID: -1}} {
		if err := ValidateLifecycleIssuePayload(p); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
