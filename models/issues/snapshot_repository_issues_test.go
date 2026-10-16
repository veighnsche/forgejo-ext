// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues

import (
	"testing"
)

// P5 narrow native proof (FW29): repository_issues page-shape validation.
// Complete means fully enumerated through the final page; an empty
// incomplete page is invalid.
func TestNewRepositoryIssuesPageTerminalRules(t *testing.T) {
	items := []RepositoryIssuesItem{{IssueID: 11, Index: 5, Visible: true}}
	if _, err := NewRepositoryIssuesPage(nil, "", true); err != nil {
		t.Fatalf("empty terminal page rejected: %v", err)
	}
	if _, err := NewRepositoryIssuesPage(items, "", true); err != nil {
		t.Fatalf("nonempty terminal page rejected: %v", err)
	}
	if _, err := NewRepositoryIssuesPage(items, "5", false); err != nil {
		t.Fatalf("nonterminal page rejected: %v", err)
	}
	bad := []struct {
		name   string
		items  []RepositoryIssuesItem
		cursor string
		full   bool
	}{
		{"empty incomplete", nil, "", false},
		{"empty incomplete with cursor", nil, "5", false},
		{"terminal with cursor", items, "5", true},
		{"nonterminal without cursor", items, "", false},
		{"nonterminal cursor mismatch", items, "6", false},
		{"non-locator index", []RepositoryIssuesItem{{IssueID: 11, Index: 0, Visible: true}}, "", true},
		{"non-locator id", []RepositoryIssuesItem{{IssueID: 0, Index: 5, Visible: true}}, "", true},
		{"hidden item", []RepositoryIssuesItem{{IssueID: 11, Index: 5}}, "", true},
		{"descending", []RepositoryIssuesItem{{IssueID: 11, Index: 6, Visible: true}, {IssueID: 12, Index: 5, Visible: true}}, "5", false},
		{"duplicate index", []RepositoryIssuesItem{{IssueID: 11, Index: 5, Visible: true}, {IssueID: 12, Index: 5, Visible: true}}, "5", false},
		{"zero cursor", items, "0", false},
		{"padded cursor", items, "05", false},
		{"text cursor", items, "nope", false},
	}
	for _, tc := range bad {
		if _, err := NewRepositoryIssuesPage(tc.items, tc.cursor, tc.full); err == nil {
			t.Fatalf("%s accepted", tc.name)
		}
	}
}
