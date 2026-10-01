// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateMergePayload(t *testing.T) {
	valid := MergePayload{
		PullRequestNumber: 3,
		HeadRepositoryID:  7,
		HeadRef:           "refs/heads/feature",
		BaseRef:           "refs/heads/main",
		ExpectedHeadOID:   strings.Repeat("1", 40),
		ExpectedBaseOID:   strings.Repeat("2", 40),
		Method:            MergeMethodFastForwardOnly,
	}
	if err := ValidateMergePayload(7, valid); err != nil {
		t.Fatalf("valid merge payload refused: %v", err)
	}

	cases := map[string]MergePayload{
		"zero pr number": func() MergePayload { p := valid; p.PullRequestNumber = 0; return p }(),
		"fork source":    func() MergePayload { p := valid; p.HeadRepositoryID = 9; return p }(),
		"identical refs": func() MergePayload { p := valid; p.BaseRef = "refs/heads/feature"; return p }(),
		"short head ref": func() MergePayload { p := valid; p.HeadRef = "feature"; return p }(),
		"bad head oid":   func() MergePayload { p := valid; p.ExpectedHeadOID = "xyz"; return p }(),
		"bad base oid":   func() MergePayload { p := valid; p.ExpectedBaseOID = "xyz"; return p }(),
		"equal oids":     func() MergePayload { p := valid; p.ExpectedBaseOID = strings.Repeat("1", 40); return p }(),
		"other method":   func() MergePayload { p := valid; p.Method = "merge"; return p }(),
		"empty method":   func() MergePayload { p := valid; p.Method = ""; return p }(),
	}
	for name, payload := range cases {
		if err := ValidateMergePayload(7, payload); !errors.Is(err, ErrInvalidMergePayload) {
			t.Fatalf("%s: expected ErrInvalidMergePayload, got %v", name, err)
		}
	}
}
