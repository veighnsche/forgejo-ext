// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"strings"
	"testing"
)

func TestValidateSnapshotRequestReviewThreads(t *testing.T) {
	byIssue := SnapshotRequest{
		RepositoryID: "3", ActorID: "7",
		Families:   []string{SnapshotFamilyReviewThreads},
		IssueIndex: "5",
	}
	if err := ValidateSnapshotRequest(byIssue); err != nil {
		t.Fatalf("threads by issue refused: %v", err)
	}
	byPull := SnapshotRequest{
		RepositoryID: "3", ActorID: "7",
		Families:   []string{SnapshotFamilyReviewThreads, SnapshotFamilyReviews},
		PullNumber: "9",
	}
	if err := ValidateSnapshotRequest(byPull); err != nil {
		t.Fatalf("threads by pull refused: %v", err)
	}
	bad := []SnapshotRequest{
		{
			RepositoryID: "3", ActorID: "7",
			Families: []string{SnapshotFamilyReviewThreads},
		},
		{
			RepositoryID: "3", ActorID: "7",
			Families:   []string{SnapshotFamilyReviewThreads},
			IssueIndex: "5", PullNumber: "9",
		},
	}
	for i, req := range bad {
		if err := ValidateSnapshotRequest(req); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestValidateSnapshotRequestMergeEligibility(t *testing.T) {
	good := SnapshotRequest{
		RepositoryID: "3", ActorID: "7",
		Families:   []string{SnapshotFamilyMergeEligibility, SnapshotFamilyPull},
		PullNumber: "9",
	}
	if err := ValidateSnapshotRequest(good); err != nil {
		t.Fatalf("eligibility with pull refused: %v", err)
	}
	withoutPull := SnapshotRequest{
		RepositoryID: "3", ActorID: "7",
		Families: []string{SnapshotFamilyMergeEligibility},
	}
	if err := ValidateSnapshotRequest(withoutPull); err == nil {
		t.Fatal("eligibility without pull accepted")
	}
}

func TestRenderThreadLocationVector(t *testing.T) {
	if got := RenderThreadLocation("review_native.go", "new", 42); got != "review_native.go|new|42" {
		t.Fatalf("thread location diverged: %q", got)
	}
	if ThreadResolverState(0) != ThreadResolverOpen {
		t.Fatal("zero resolve-doer is not open")
	}
	if ThreadResolverState(7) != ThreadResolverResolved {
		t.Fatal("set resolve-doer is not resolved")
	}
}

func TestThreadDigestBindsLocationAndResolverState(t *testing.T) {
	location := RenderThreadLocation("a.go", "old", 7)
	lines := []string{
		ResolveConversationLine(ResolveConversationMember{ID: 11, Body: "needs a bounds check", ContentVer: 1, Location: location, ResolverState: ThreadResolverOpen}),
		ResolveConversationLine(ResolveConversationMember{ID: 12, Body: "added in the next push", ContentVer: 2, Location: location, ResolverState: ThreadResolverResolved}),
	}
	digest, err := ResolveConversationDigest(lines)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != 64 || !strings.Contains("0123456789abcdef", string(digest[0])) {
		t.Fatalf("thread digest malformed: %q", digest)
	}
	// Any resolver-state change alters the digest and refuses the stale
	// resolution.
	flipped := []string{
		ResolveConversationLine(ResolveConversationMember{ID: 11, Body: "needs a bounds check", ContentVer: 1, Location: location, ResolverState: ThreadResolverResolved}),
		ResolveConversationLine(ResolveConversationMember{ID: 12, Body: "added in the next push", ContentVer: 2, Location: location, ResolverState: ThreadResolverResolved}),
	}
	other, err := ResolveConversationDigest(flipped)
	if err != nil {
		t.Fatal(err)
	}
	if other == digest {
		t.Fatal("resolver-state change kept the digest")
	}
}
