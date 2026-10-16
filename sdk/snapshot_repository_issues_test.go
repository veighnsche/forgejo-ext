// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"strings"
	"testing"
)

// P5 narrow native proof (FW29): generic repository_issues request bounds.
// The family is requested alone with only Limit/Cursor selectors; the
// cursor is an exclusive issue-index lower bound.
func TestValidateRepositoryIssuesRequestBounds(t *testing.T) {
	good := []SnapshotRequest{
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, Limit: 50},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, Limit: 1, Cursor: "7"},
	}
	for i, req := range good {
		if err := ValidateSnapshotRequest(req); err != nil {
			t.Fatalf("case %d rejected: %v", i, err)
		}
	}
	bad := []SnapshotRequest{
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, IssueIndex: "5"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, PullNumber: "9"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, CommentIDs: []string{"11"}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, SHA: snapshotTestSHA},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, Refs: []string{"refs/heads/main"}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, WriterIDs: []string{"7"}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, Ancestry: &SnapshotAncestryRequest{AncestorOID: snapshotTestSHA, DescendantOID: strings.Repeat("a", 40)}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues, SnapshotFamilyIssue}, IssueIndex: "5"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, Limit: 51},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, Cursor: "not-an-id"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, Cursor: "0"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, Cursor: "007"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRepositoryIssues}, Cursor: strings.Repeat("1", SnapshotCursorLimit+1)},
	}
	for i, req := range bad {
		if err := ValidateSnapshotRequest(req); !errors.Is(err, ErrInvalidSnapshotRequest) {
			t.Fatalf("case %d accepted: %v", i, err)
		}
	}
}
