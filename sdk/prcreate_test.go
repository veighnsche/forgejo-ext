// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePRCreatePayload(t *testing.T) {
	valid := PRCreatePayload{
		HeadRepositoryID: 7,
		HeadRef:          "refs/heads/feature",
		BaseRef:          "refs/heads/main",
		ExpectedHeadOID:  strings.Repeat("1", 40),
		ExpectedBaseOID:  strings.Repeat("2", 40),
		Title:            "exact title",
		Body:             "factory body",
	}
	if err := ValidatePRCreatePayload(7, valid); err != nil {
		t.Fatalf("valid pr-create payload refused: %v", err)
	}

	cases := map[string]PRCreatePayload{
		"fork source":      func() PRCreatePayload { p := valid; p.HeadRepositoryID = 9; return p }(),
		"identical refs":   func() PRCreatePayload { p := valid; p.HeadRef = "refs/heads/main"; return p }(),
		"short head ref":   func() PRCreatePayload { p := valid; p.HeadRef = "feature"; return p }(),
		"bad head oid":     func() PRCreatePayload { p := valid; p.ExpectedHeadOID = "xyz"; return p }(),
		"equal oids":       func() PRCreatePayload { p := valid; p.ExpectedBaseOID = strings.Repeat("1", 40); return p }(),
		"blank title":      func() PRCreatePayload { p := valid; p.Title = "  "; return p }(),
		"over-limit title": func() PRCreatePayload { p := valid; p.Title = strings.Repeat("t", 256); return p }(),
		"maintainer edit":  func() PRCreatePayload { p := valid; p.AllowMaintainerEdit = true; return p }(),
	}
	for name, payload := range cases {
		if err := ValidatePRCreatePayload(7, payload); !errors.Is(err, ErrInvalidPRCreatePayload) {
			t.Fatalf("%s: expected ErrInvalidPRCreatePayload, got %v", name, err)
		}
	}
}
