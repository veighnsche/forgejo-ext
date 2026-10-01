// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"strings"
	"testing"
)

const snapshotTestSHA = "0123456789abcdef0123456789abcdef01234567"

func snapshotTestRequest() SnapshotRequest {
	return SnapshotRequest{
		RepositoryID: "3",
		ActorID:      "7",
		Families:     []string{SnapshotFamilyIssue, SnapshotFamilyDependencies, SnapshotFamilyChecks, SnapshotFamilyRefs},
		IssueIndex:   "5",
		SHA:          snapshotTestSHA,
		Refs:         []string{"refs/heads/candidate"},
	}
}

func TestValidateSnapshotRequestAcceptsBoundedSelectors(t *testing.T) {
	good := []SnapshotRequest{
		snapshotTestRequest(),
		{
			RepositoryID: "3", ActorID: "7",
			Families:   []string{SnapshotFamilyComments},
			CommentIDs: []string{"11", "12"},
		},
		{
			RepositoryID: "3", ActorID: "7",
			Families:   []string{SnapshotFamilyComments, SnapshotFamilyReviews},
			IssueIndex: "5",
		},
		{
			RepositoryID: "3", ActorID: "7",
			Families:   []string{SnapshotFamilyPull, SnapshotFamilyReviews},
			PullNumber: "9",
		},
	}
	for i, req := range good {
		if err := ValidateSnapshotRequest(req); err != nil {
			t.Fatalf("case %d rejected: %v", i, err)
		}
	}
}

func TestValidateSnapshotRequestRefusesMalformedSelectors(t *testing.T) {
	bad := []SnapshotRequest{
		{},
		{RepositoryID: "0", ActorID: "7", Families: []string{SnapshotFamilyIssue}, IssueIndex: "5"},
		{RepositoryID: "3", ActorID: "0", Families: []string{SnapshotFamilyIssue}, IssueIndex: "5"},
		{RepositoryID: "3", ActorID: "7"},
		{RepositoryID: "3", ActorID: "7", Families: []string{"approval"}, IssueIndex: "5"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyIssue, SnapshotFamilyIssue}, IssueIndex: "5"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRefs}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyChecks}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyIssue}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyDependencies}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyPull}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyComments}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyReviews}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyComments}, IssueIndex: "5", CommentIDs: []string{"11"}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyReviews}, IssueIndex: "5", PullNumber: "9"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyIssue}, IssueIndex: "abc"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyIssue}, IssueIndex: "5", Limit: 500},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyChecks}, SHA: "short"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyRefs}, Refs: []string{"candidate"}},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyIssue}, IssueIndex: "5", Cursor: "not-an-id"},
		{RepositoryID: "3", ActorID: "7", Families: []string{SnapshotFamilyIssue}, IssueIndex: "5", Cursor: strings.Repeat("1", SnapshotCursorLimit+1)},
	}
	for i, req := range bad {
		if err := ValidateSnapshotRequest(req); !errors.Is(err, ErrInvalidSnapshotRequest) {
			t.Fatalf("case %d accepted: %v", i, err)
		}
	}
}
