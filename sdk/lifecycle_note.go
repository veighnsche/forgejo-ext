// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// MaxLifecycleNoteChars caps one completion note per event.
const MaxLifecycleNoteChars = 1000

// LifecycleNotePayload is the issue.comment.publish intent payload: one
// bounded completion note on one exact original issue with its merge
// evidence reference. The host validates authoritatively; the SDK
// validation below only catches submitter bugs early.
type LifecycleNotePayload struct {
	IssueID  int64  `json:"issue_id"`
	Content  string `json:"content"`
	MergeRef string `json:"merge_ref"`
}

// ErrInvalidLifecycleNotePayload reports a malformed note payload.
var ErrInvalidLifecycleNotePayload = errors.New("invalid lifecycle note payload")

// ValidateLifecycleNotePayload checks the structural intent rules: a
// positive issue ID, nonempty merge evidence and content of 1..1000
// characters.
func ValidateLifecycleNotePayload(p LifecycleNotePayload) error {
	if p.IssueID <= 0 || p.MergeRef == "" || len(p.MergeRef) > 512 {
		return ErrInvalidLifecycleNotePayload
	}
	if strings.TrimSpace(p.Content) == "" || utf8.RuneCountInString(p.Content) > MaxLifecycleNoteChars || strings.ContainsRune(p.Content, 0) || !utf8.ValidString(p.Content) {
		return ErrInvalidLifecycleNotePayload
	}
	return nil
}
