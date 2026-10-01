// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateReviewSubmitPayload(t *testing.T) {
	valid := ReviewSubmitPayload{
		PullRequestNumber: 3,
		PRAuthorID:        1,
		HeadRepositoryID:  7,
		HeadRef:           "refs/heads/feature",
		BaseRef:           "refs/heads/main",
		ExpectedHeadOID:   strings.Repeat("1", 40),
		ExpectedBaseOID:   strings.Repeat("2", 40),
		CommitID:          strings.Repeat("1", 40),
		Event:             ReviewSubmitEventApproved,
		Body:              "looks good",
	}
	if err := ValidateReviewSubmitPayload(7, valid); err != nil {
		t.Fatalf("valid review-submit payload refused: %v", err)
	}
	approveEmpty := valid
	approveEmpty.Body = ""
	if err := ValidateReviewSubmitPayload(7, approveEmpty); err != nil {
		t.Fatalf("empty-body approval refused: %v", err)
	}
	reject := valid
	reject.Event = ReviewSubmitEventRequestChanges
	reject.Body = "please fix"
	if err := ValidateReviewSubmitPayload(7, reject); err != nil {
		t.Fatalf("valid request-changes payload refused: %v", err)
	}

	cases := map[string]ReviewSubmitPayload{
		"zero pr number": func() ReviewSubmitPayload { p := valid; p.PullRequestNumber = 0; return p }(),
		"zero author":    func() ReviewSubmitPayload { p := valid; p.PRAuthorID = 0; return p }(),
		"fork source":    func() ReviewSubmitPayload { p := valid; p.HeadRepositoryID = 9; return p }(),
		"identical refs": func() ReviewSubmitPayload { p := valid; p.BaseRef = "refs/heads/feature"; return p }(),
		"short head ref": func() ReviewSubmitPayload { p := valid; p.HeadRef = "feature"; return p }(),
		"bad head oid":   func() ReviewSubmitPayload { p := valid; p.ExpectedHeadOID = "xyz"; return p }(),
		"bad base oid":   func() ReviewSubmitPayload { p := valid; p.ExpectedBaseOID = "xyz"; return p }(),
		"commit differs": func() ReviewSubmitPayload { p := valid; p.CommitID = strings.Repeat("2", 40); return p }(),
		"comment event":  func() ReviewSubmitPayload { p := valid; p.Event = "COMMENT"; return p }(),
		"pending event":  func() ReviewSubmitPayload { p := valid; p.Event = "PENDING"; return p }(),
		"empty event":    func() ReviewSubmitPayload { p := valid; p.Event = ""; return p }(),
		"reject without body": func() ReviewSubmitPayload {
			p := valid
			p.Event = ReviewSubmitEventRequestChanges
			p.Body = "  "
			return p
		}(),
	}
	for name, payload := range cases {
		if err := ValidateReviewSubmitPayload(7, payload); !errors.Is(err, ErrInvalidReviewSubmitPayload) {
			t.Fatalf("%s: expected ErrInvalidReviewSubmitPayload, got %v", name, err)
		}
	}
}
