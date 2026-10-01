// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prCreatePayload(headRepo int64, headRef, baseRef, headOID, baseOID, title string, maintainerEdit bool) string {
	return fmt.Sprintf(`{"head_repository_id":%d,`+
		`"head_ref":%q,"base_ref":%q,`+
		`"expected_head_oid":%q,"expected_base_oid":%q,`+
		`"title":%q,"body":"factory body",`+
		`"allow_maintainer_edit":%t}`,
		headRepo, headRef, baseRef, headOID, baseOID, title, maintainerEdit)
}

func TestParsePRCreatePayloadAcceptsExactInput(t *testing.T) {
	head := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	pr, err := parsePRCreatePayload([]byte(prCreatePayload(7,
		"refs/heads/feature", "refs/heads/main",
		strings.ToUpper(head), strings.ToUpper(base),
		"exact title", false)), 7)
	require.NoError(t, err)
	assert.Equal(t, int64(7), pr.HeadRepositoryID)
	assert.Equal(t, "refs/heads/feature", pr.HeadRef)
	assert.Equal(t, "refs/heads/main", pr.BaseRef)
	assert.Equal(t, head, pr.ExpectedHeadOID)
	assert.Equal(t, base, pr.ExpectedBaseOID)
	assert.Equal(t, "exact title", pr.Title)
	assert.False(t, pr.AllowMaintainerEdit)
}

func TestParsePRCreatePayloadRejects(t *testing.T) {
	head := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	valid := func() string {
		return prCreatePayload(7, "refs/heads/feature", "refs/heads/main", head, base, "title", false)
	}
	cases := map[string]string{
		"empty":            "",
		"unknown field":    `{"head_repository_id":7,"head_ref":"refs/heads/feature","base_ref":"refs/heads/main","expected_head_oid":"` + head + `","expected_base_oid":"` + base + `","title":"t","labels":[1]}`,
		"trailing data":    valid() + `{}`,
		"fork source":      prCreatePayload(9, "refs/heads/feature", "refs/heads/main", head, base, "title", false),
		"identical refs":   prCreatePayload(7, "refs/heads/main", "refs/heads/main", head, base, "title", false),
		"short head ref":   prCreatePayload(7, "feature", "refs/heads/main", head, base, "title", false),
		"bad head oid":     prCreatePayload(7, "refs/heads/feature", "refs/heads/main", "xyz", base, "title", false),
		"equal oids":       prCreatePayload(7, "refs/heads/feature", "refs/heads/main", head, head, "title", false),
		"blank title":      prCreatePayload(7, "refs/heads/feature", "refs/heads/main", head, base, "  ", false),
		"over-limit title": prCreatePayload(7, "refs/heads/feature", "refs/heads/main", head, base, strings.Repeat("t", 256), false),
		"maintainer edit":  prCreatePayload(7, "refs/heads/feature", "refs/heads/main", head, base, "title", true),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parsePRCreatePayload([]byte(payload), 7)
			assert.ErrorIs(t, err, ErrInvalidIntent)
		})
	}
}
