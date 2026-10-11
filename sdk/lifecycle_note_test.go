// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"strings"
	"testing"
)

func TestValidateLifecycleNotePayload(t *testing.T) {
	if err := ValidateLifecycleNotePayload(LifecycleNotePayload{IssueID: 12, Content: "done", MergeRef: "pr/88@abc"}); err != nil {
		t.Fatalf("valid note rejected: %v", err)
	}
	for i, p := range []LifecycleNotePayload{{}, {IssueID: 12, Content: "x"}, {IssueID: 12, MergeRef: "m"}, {IssueID: 12, Content: strings.Repeat("x", MaxLifecycleNoteChars+1), MergeRef: "m"}} {
		if err := ValidateLifecycleNotePayload(p); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
