// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"testing"
)

func TestValidateLifecycleReviewPayload(t *testing.T) {
	if err := ValidateLifecycleReviewPayload(LifecycleReviewPayload{IssueID: 88, ReviewerIDs: []int64{15, 23, 29}}); err != nil {
		t.Fatalf("three reviewers rejected: %v", err)
	}
	for i, p := range []LifecycleReviewPayload{{}, {IssueID: 88}, {IssueID: 88, ReviewerIDs: []int64{1, 2, 3, 4}}, {IssueID: 88, ReviewerIDs: []int64{15, 15}}} {
		if err := ValidateLifecycleReviewPayload(p); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
