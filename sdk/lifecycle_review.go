// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
)

// MaxLifecycleReviewers caps human reviewer requests per PR/event.
const MaxLifecycleReviewers = 3

// LifecycleReviewPayload is the pull_request.review.request intent
// payload: up to three human reviewers on one exact PR. The host
// validates authoritatively; the SDK validation below only catches
// submitter bugs early.
type LifecycleReviewPayload struct {
	IssueID     int64   `json:"issue_id"`
	ReviewerIDs []int64 `json:"reviewer_ids"`
}

// ErrInvalidLifecycleReviewPayload reports a malformed reviewer payload.
var ErrInvalidLifecycleReviewPayload = errors.New("invalid lifecycle review payload")

// ValidateLifecycleReviewPayload checks the structural intent rules: a
// positive issue ID and one to three distinct positive reviewer IDs.
func ValidateLifecycleReviewPayload(p LifecycleReviewPayload) error {
	if p.IssueID <= 0 || len(p.ReviewerIDs) == 0 || len(p.ReviewerIDs) > MaxLifecycleReviewers {
		return ErrInvalidLifecycleReviewPayload
	}
	seen := map[int64]bool{}
	for _, id := range p.ReviewerIDs {
		if id <= 0 || seen[id] {
			return ErrInvalidLifecycleReviewPayload
		}
		seen[id] = true
	}
	return nil
}
