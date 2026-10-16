// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues

import (
	"strings"
	"testing"

	"forgejo.org/modules/timeutil"
)

func threadTestComment(id, reviewID int64, line int64, created int64) *Comment {
	return &Comment{
		ID: id, IssueID: 9, PosterID: 2, Type: CommentTypeCode,
		ReviewID: reviewID, TreePath: "a.go", Line: line,
		Content: "body", ContentVersion: 1,
		CommitSHA:   strings.Repeat("1", 40),
		CreatedUnix: timeutil.TimeStamp(created),
		UpdatedUnix: timeutil.TimeStamp(created),
	}
}

func TestGroupCodeThreadsRootsAndMembers(t *testing.T) {
	// Ungrouped comments (no review) never form threads.
	ungrouped := threadTestComment(50, 0, 7, 1700000050)
	comments := CommentList{
		threadTestComment(30, 5, 7, 1700000030),
		threadTestComment(10, 5, 7, 1700000010),
		threadTestComment(20, 5, -7, 1700000020),
		threadTestComment(40, 6, 7, 1700000040),
		ungrouped,
	}
	threads := GroupCodeThreads(comments)
	if len(threads) != 3 {
		t.Fatalf("grouped %d threads, want 3", len(threads))
	}
	// Native (created, id) order across threads; root first within.
	if threads[0].Root().ID != 10 || len(threads[0]) != 2 {
		t.Fatalf("first thread root=%v members=%d", threads[0].Root().ID, len(threads[0]))
	}
	if threads[0][1].ID != 30 {
		t.Fatalf("thread member order wrong: %d", threads[0][1].ID)
	}
	if threads[1].Root().ID != 20 {
		t.Fatalf("signed line splits the group: root=%d", threads[1].Root().ID)
	}
	if threads[2].Root().ID != 40 {
		t.Fatalf("review splits the group: root=%d", threads[2].Root().ID)
	}
	if got := FindCodeThread(threads, 30); got == nil || got.Root().ID != 10 {
		t.Fatal("member lookup missed its thread")
	}
	if got := FindCodeThread(threads, 99); got != nil {
		t.Fatal("unknown comment matched a thread")
	}
}

func threadTestMembers() []ThreadMemberSnapshot {
	return []ThreadMemberSnapshot{
		{CommentID: 10, IssueID: 9, ContentDigest: ContentDigest("body"), ContentVersion: 1, CreatedUnix: 1700000010, UpdatedUnix: 1700000010, Visible: true, Complete: true},
		{CommentID: 30, IssueID: 9, ContentDigest: ContentDigest("reply"), ContentVersion: 2, CreatedUnix: 1700000030, UpdatedUnix: 1700000030, Visible: true, Complete: true},
	}
}

func TestNewThreadSnapshotRoundTrip(t *testing.T) {
	root := threadTestComment(10, 5, 7, 1700000010)
	got, err := NewThreadSnapshot(root, 9, 5, "a.go", "new", 7,
		strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("a", 64),
		threadTestMembers(), 2, 0, false, true, true, "", true)
	if err != nil {
		t.Fatalf("valid thread refused: %v", err)
	}
	if got.RootID != 10 || got.Side != "new" || got.Line != 7 || got.MemberTotal != 2 {
		t.Fatalf("thread converted wrong: %+v", got)
	}
	if got.Resolved || got.ResolverID != 0 {
		t.Fatal("open thread reports a resolver")
	}
	if !got.Outdated {
		t.Fatal("commit drift must mark the thread outdated")
	}
}

func TestNewThreadSnapshotRefuses(t *testing.T) {
	root := threadTestComment(10, 5, 7, 1700000010)
	members := threadTestMembers()
	cases := map[string]func() error{
		"bad side": func() error {
			_, err := NewThreadSnapshot(root, 9, 5, "a.go", "sideways", 7, "", "", strings.Repeat("a", 64), members, 2, 0, false, false, true, "", true)
			return err
		},
		"zero line": func() error {
			_, err := NewThreadSnapshot(root, 9, 5, "a.go", "new", 0, "", "", strings.Repeat("a", 64), members, 2, 0, false, false, true, "", true)
			return err
		},
		"short digest": func() error {
			_, err := NewThreadSnapshot(root, 9, 5, "a.go", "new", 7, "", "", "abc", members, 2, 0, false, false, true, "", true)
			return err
		},
		"resolver mismatch": func() error {
			_, err := NewThreadSnapshot(root, 9, 5, "a.go", "new", 7, "", "", strings.Repeat("a", 64), members, 2, 0, true, false, true, "", true)
			return err
		},
		"complete short": func() error {
			_, err := NewThreadSnapshot(root, 9, 5, "a.go", "new", 7, "", "", strings.Repeat("a", 64), members[:1], 2, 0, false, false, true, "", true)
			return err
		},
	}
	for name, fn := range cases {
		if err := fn(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	hidden, err := NewThreadSnapshot(root, 9, 5, "", "", 0, "", "", "", nil, 0, 0, false, false, false, HiddenReasonRedacted, true)
	if err != nil {
		t.Fatalf("hidden thread refused: %v", err)
	}
	if hidden.Visible || hidden.Digest != "" || len(hidden.Members) != 0 {
		t.Fatal("hidden thread leaks evidence")
	}
}

func TestNewThreadPageCursorRules(t *testing.T) {
	root := threadTestComment(10, 5, 7, 1700000010)
	mk := func(id int64) ThreadSnapshot {
		item, err := NewThreadSnapshot(&Comment{ID: id}, 9, 5, "a.go", "new", 7, "", "", strings.Repeat("a", 64), nil, 0, 0, false, false, true, "", true)
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	_ = root
	terminal, err := NewThreadPage(9, []ThreadSnapshot{mk(10), mk(30)}, "", 2, true)
	if err != nil {
		t.Fatalf("terminal page refused: %v", err)
	}
	if !terminal.Complete || terminal.NextCursor != "" {
		t.Fatal("terminal page misreported")
	}
	nonterminal, err := NewThreadPage(9, []ThreadSnapshot{mk(10)}, "10", 2, false)
	if err != nil {
		t.Fatalf("nonterminal page refused: %v", err)
	}
	if nonterminal.Complete || nonterminal.NextCursor != "10" {
		t.Fatal("nonterminal page misreported")
	}
	// Terminal-incomplete: everything remaining observed but blocked.
	blocked, err := NewThreadPage(9, []ThreadSnapshot{mk(10)}, "", 1, false)
	if err != nil {
		t.Fatalf("terminal-incomplete page refused: %v", err)
	}
	if blocked.Complete || blocked.NextCursor != "" {
		t.Fatal("terminal-incomplete page misreported")
	}
	for name, fn := range map[string]func() error{
		"complete with cursor": func() error {
			_, err := NewThreadPage(9, []ThreadSnapshot{mk(10)}, "10", 1, true)
			return err
		},
		"complete short": func() error {
			_, err := NewThreadPage(9, []ThreadSnapshot{mk(10)}, "", 2, true)
			return err
		},
		"cursor off last root": func() error {
			_, err := NewThreadPage(9, []ThreadSnapshot{mk(10)}, "30", 2, false)
			return err
		},
		"empty incomplete": func() error {
			_, err := NewThreadPage(9, nil, "", 1, false)
			return err
		},
		"unordered roots": func() error {
			_, err := NewThreadPage(9, []ThreadSnapshot{mk(30), mk(10)}, "", 2, true)
			return err
		},
	} {
		if err := fn(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestNewMergeEligibilitySnapshotVerdicts(t *testing.T) {
	pr := &PullRequest{ID: 2, IssueID: 3, Index: 3}
	eligible, err := NewMergeEligibilitySnapshot(pr, strings.Repeat("1", 40), strings.Repeat("2", 40), true, "", nil, true, "", true)
	if err != nil {
		t.Fatalf("eligible verdict refused: %v", err)
	}
	if !eligible.Eligible || eligible.Blocker != "" {
		t.Fatal("eligible verdict misreported")
	}
	blocked, err := NewMergeEligibilitySnapshot(pr, strings.Repeat("1", 40), strings.Repeat("2", 40), false, MergeBlockedApprovals, nil, true, "", true)
	if err != nil {
		t.Fatalf("blocked verdict refused: %v", err)
	}
	if blocked.Eligible || blocked.Blocker != "approvals" {
		t.Fatal("blocked verdict misreported")
	}
	for name, fn := range map[string]func() error{
		"eligible with blocker": func() error {
			_, err := NewMergeEligibilitySnapshot(pr, "", "", true, MergeBlockedApprovals, nil, true, "", true)
			return err
		},
		"unknown blocker": func() error {
			_, err := NewMergeEligibilitySnapshot(pr, "", "", false, "vibes", nil, true, "", true)
			return err
		},
		"duplicate review ids": func() error {
			_, err := NewMergeEligibilitySnapshot(pr, "", "", false, MergeBlockedRequestedChanges, []int64{4, 4}, true, "", true)
			return err
		},
	} {
		if err := fn(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	hidden, err := NewMergeEligibilitySnapshot(pr, "", "", false, "", nil, false, HiddenReasonNoAccess, true)
	if err != nil {
		t.Fatalf("hidden eligibility refused: %v", err)
	}
	if hidden.Visible || hidden.Eligible {
		t.Fatal("hidden eligibility leaks a verdict")
	}
}
