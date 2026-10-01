// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reviewSubmitPayload(number, author, headRepo int64, headRef, baseRef, headOID, baseOID, commit, event, body string) string {
	return fmt.Sprintf(`{"pull_request_number":%d,"pr_author_id":%d,`+
		`"head_repository_id":%d,`+
		`"head_ref":%q,"base_ref":%q,`+
		`"expected_head_oid":%q,"expected_base_oid":%q,`+
		`"commit_id":%q,"event":%q,"body":%q}`,
		number, author, headRepo, headRef, baseRef, headOID, baseOID, commit, event, body)
}

func TestParseReviewSubmitPayloadAcceptsExactInput(t *testing.T) {
	head := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	review, err := parseReviewSubmitPayload([]byte(reviewSubmitPayload(3, 1, 7,
		"refs/heads/feature", "refs/heads/main",
		strings.ToUpper(head), strings.ToUpper(base), strings.ToUpper(head),
		"APPROVED", "looks good")), 7)
	require.NoError(t, err)
	assert.Equal(t, int64(3), review.PullRequestNumber)
	assert.Equal(t, int64(1), review.PRAuthorID)
	assert.Equal(t, int64(7), review.HeadRepositoryID)
	assert.Equal(t, "refs/heads/feature", review.HeadRef)
	assert.Equal(t, "refs/heads/main", review.BaseRef)
	assert.Equal(t, head, review.ExpectedHeadOID)
	assert.Equal(t, base, review.ExpectedBaseOID)
	assert.Equal(t, head, review.CommitID)
	assert.Equal(t, "APPROVED", review.Event)
	assert.Equal(t, "looks good", review.Body)
	assert.Equal(t, issues_model.ReviewTypeApprove, reviewSubmitType(review.Event))
	assert.Equal(t, issues_model.ReviewTypeReject, reviewSubmitType("REQUEST_CHANGES"))
}

func TestParseReviewSubmitPayloadRejects(t *testing.T) {
	head := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	valid := func() string {
		return reviewSubmitPayload(3, 1, 7, "refs/heads/feature", "refs/heads/main", head, base, head, "APPROVED", "looks good")
	}
	cases := map[string]string{
		"empty":          "",
		"unknown field":  `{"pull_request_number":3,"pr_author_id":1,"head_repository_id":7,"head_ref":"refs/heads/feature","base_ref":"refs/heads/main","expected_head_oid":"` + head + `","expected_base_oid":"` + base + `","commit_id":"` + head + `","event":"APPROVED","body":"b","comments":[]}`,
		"trailing data":  valid() + `{}`,
		"zero pr number": reviewSubmitPayload(0, 1, 7, "refs/heads/feature", "refs/heads/main", head, base, head, "APPROVED", "b"),
		"zero author":    reviewSubmitPayload(3, 0, 7, "refs/heads/feature", "refs/heads/main", head, base, head, "APPROVED", "b"),
		"fork source":    reviewSubmitPayload(3, 1, 9, "refs/heads/feature", "refs/heads/main", head, base, head, "APPROVED", "b"),
		"identical refs": reviewSubmitPayload(3, 1, 7, "refs/heads/main", "refs/heads/main", head, base, head, "APPROVED", "b"),
		"short head ref": reviewSubmitPayload(3, 1, 7, "feature", "refs/heads/main", head, base, head, "APPROVED", "b"),
		"bad head oid":   reviewSubmitPayload(3, 1, 7, "refs/heads/feature", "refs/heads/main", "xyz", base, head, "APPROVED", "b"),
		"bad base oid":   reviewSubmitPayload(3, 1, 7, "refs/heads/feature", "refs/heads/main", head, "xyz", head, "APPROVED", "b"),
		"commit differs": reviewSubmitPayload(3, 1, 7, "refs/heads/feature", "refs/heads/main", head, base, base, "APPROVED", "b"),
		"comment event":  reviewSubmitPayload(3, 1, 7, "refs/heads/feature", "refs/heads/main", head, base, head, "COMMENT", "b"),
		"pending event":  reviewSubmitPayload(3, 1, 7, "refs/heads/feature", "refs/heads/main", head, base, head, "PENDING", "b"),
		"empty event":    reviewSubmitPayload(3, 1, 7, "refs/heads/feature", "refs/heads/main", head, base, head, "", "b"),
		"reject no body": reviewSubmitPayload(3, 1, 7, "refs/heads/feature", "refs/heads/main", head, base, head, "REQUEST_CHANGES", "  "),
	}
	for name, payload := range cases {
		_, err := parseReviewSubmitPayload([]byte(payload), 7)
		assert.ErrorIs(t, err, ErrInvalidIntent, name)
	}

	reject := reviewSubmitPayload(3, 1, 7, "refs/heads/feature", "refs/heads/main", head, base, head, "REQUEST_CHANGES", "please fix")
	parsed, err := parseReviewSubmitPayload([]byte(reject), 7)
	require.NoError(t, err)
	assert.Equal(t, "REQUEST_CHANGES", parsed.Event)
}

func TestMapExactReviewRefusal(t *testing.T) {
	cases := map[string]string{
		"head changed":                                 model.ReasonStaleHead,
		"base changed":                                 model.ReasonStaleBaseOrResult,
		"pending draft exists":                         model.ReasonPendingReviewExists,
		"cannot review your own pull request":          model.ReasonAuthorityLost,
		"reviewer is blocked":                          model.ReasonAuthorityLost,
		"reviewer cannot read this pull request":       model.ReasonAuthorityLost,
		"pull request is closed or merged":             model.ReasonPRMismatch,
		"pr author mismatch":                           model.ReasonPRMismatch,
		"head branch mismatch":                         model.ReasonPRMismatch,
		"invalid object id":                            model.ReasonNativeRefused,
		"commit must equal the expected head":          model.ReasonNativeRefused,
		"only approve or reject reviews are supported": model.ReasonNativeRefused,
	}
	for reason, want := range cases {
		assert.Equal(t, want, mapExactReviewRefusal(issues_model.ErrExactReviewRefused{Reason: reason}), reason)
	}
	assert.Equal(t, model.ReasonNativeRefused, mapExactReviewRefusal(issues_model.ContentEmptyErr{}))
	assert.Equal(t, model.ReasonNativeRefused, mapExactReviewRefusal(errors.New("boom")))
	assert.Equal(t, model.ReasonPRMismatch, mapExactReviewRefusal(issues_model.ErrExactReviewRefused{Reason: "future reason"}))
}
