// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"strings"
)

// Review submit events. Only final dispositions are supported: inline
// comments, attachments, pending/draft creation, submission of an existing
// draft, editing and dismissal are outside this operation. The values match
// the native API review states.
const (
	ReviewSubmitEventApproved       = "APPROVED"
	ReviewSubmitEventRequestChanges = "REQUEST_CHANGES"
)

// ReviewSubmitPayload is the pull_request.review.submit intent payload: one
// final body-only native review bound to its exact candidate. The first
// interface covers same-repository PRs only, with commit_id equal to the
// expected head. The host validates authoritatively; the SDK validation
// below only catches submitter bugs early.
type ReviewSubmitPayload struct {
	PullRequestNumber int64  `json:"pull_request_number"`
	PRAuthorID        int64  `json:"pr_author_id"`
	HeadRepositoryID  int64  `json:"head_repository_id"`
	HeadRef           string `json:"head_ref"`
	BaseRef           string `json:"base_ref"`
	ExpectedHeadOID   string `json:"expected_head_oid"`
	ExpectedBaseOID   string `json:"expected_base_oid"`
	CommitID          string `json:"commit_id"`
	Event             string `json:"event"`
	Body              string `json:"body"`
}

// ErrInvalidReviewSubmitPayload reports a malformed review-submit payload.
var ErrInvalidReviewSubmitPayload = errors.New("invalid review-submit payload")

func validReviewSubmitPayloadOID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	for _, c := range oid {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
			continue
		}
		return false
	}
	return true
}

func validReviewSubmitPayloadRef(ref string) bool {
	if !strings.HasPrefix(ref, "refs/heads/") || len(ref) > 512 {
		return false
	}
	name := strings.TrimSpace(ref)
	if name != ref || strings.ContainsAny(ref, " ~^:?*\\") || strings.Contains(ref, "..") {
		return false
	}
	return len(strings.TrimPrefix(ref, "refs/heads/")) > 0
}

// ValidateReviewSubmitPayload checks the structural review-submit intent
// rules: a positive PR number and author, a same-repository head, distinct
// full branch refs, valid exact OIDs, a commit equal to the expected head, a
// final event and a body for requested changes. The host re-validates
// everything against live native state.
func ValidateReviewSubmitPayload(repositoryID int64, payload ReviewSubmitPayload) error {
	if payload.PullRequestNumber <= 0 || payload.PRAuthorID <= 0 {
		return ErrInvalidReviewSubmitPayload
	}
	if payload.HeadRepositoryID != repositoryID {
		return ErrInvalidReviewSubmitPayload
	}
	if !validReviewSubmitPayloadRef(payload.HeadRef) || !validReviewSubmitPayloadRef(payload.BaseRef) {
		return ErrInvalidReviewSubmitPayload
	}
	if payload.HeadRef == payload.BaseRef {
		return ErrInvalidReviewSubmitPayload
	}
	if !validReviewSubmitPayloadOID(payload.ExpectedHeadOID) || !validReviewSubmitPayloadOID(payload.ExpectedBaseOID) {
		return ErrInvalidReviewSubmitPayload
	}
	if !validReviewSubmitPayloadOID(payload.CommitID) || !strings.EqualFold(payload.CommitID, payload.ExpectedHeadOID) {
		return ErrInvalidReviewSubmitPayload
	}
	switch payload.Event {
	case ReviewSubmitEventApproved:
	case ReviewSubmitEventRequestChanges:
		if strings.TrimSpace(payload.Body) == "" {
			return ErrInvalidReviewSubmitPayload
		}
	default:
		return ErrInvalidReviewSubmitPayload
	}
	return nil
}
