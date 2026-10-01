// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"fmt"
	"strings"
	"testing"
	"time"

	model "forgejo.org/models/nativeoperation"
	"forgejo.org/modules/git"

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

func TestValidateIntentCarriesPRCreatePayload(t *testing.T) {
	head := strings.Repeat("a", 40)
	base := strings.Repeat("b", 40)
	now := time.Now().Unix()
	intent, err := ValidateIntent("op-prcreate", 2, 7, model.KindPRCreate, "rev-1", 7, now+300,
		[]byte(prCreatePayload(7, "refs/heads/feature", "refs/heads/main", head, base, "title", false)), now)
	require.NoError(t, err)
	require.NotNil(t, intent.PRCreate)
	assert.Equal(t, "refs/heads/feature", intent.PRCreate.HeadRef)
	assert.Equal(t, head, intent.PRCreate.ExpectedHeadOID)
	assert.NotEmpty(t, intent.Digest)
	assert.NotEmpty(t, intent.Canonical)
}

func TestValidateIntentRejectsPRCreatePayload(t *testing.T) {
	now := time.Now().Unix()
	_, err := ValidateIntent("op-prcreate", 2, 7, model.KindPRCreate, "rev-1", 7, now+300,
		[]byte(prCreatePayload(9, "refs/heads/feature", "refs/heads/main", strings.Repeat("a", 40), strings.Repeat("b", 40), "title", false)), now)
	assert.ErrorIs(t, err, ErrInvalidIntent)
}

func TestCheckPRCreateCompletionAdmitsExactTuple(t *testing.T) {
	svc := NewService()
	head := strings.Repeat("a", 40)
	zero := git.Sha1ObjectFormat.EmptyObjectID().String()
	op := &model.Operation{
		Submitted:   true,
		Kind:        model.KindPRCreate,
		EffectState: model.EffectCommitted,
		Receipt:     `{"pr_id":7}`,
	}
	scope := Scope{
		Kind:             model.OwnerConditional,
		RepositoryID:     7,
		CompletionRef:    "refs/pull/3/head",
		CompletionNewOID: head,
		CompletionPhase:  PRCreateCompletionPhase,
	}
	decision := svc.checkPRCreateCompletion(scope, op, []RefLine{{Old: zero, New: head, Ref: "refs/pull/3/head"}})
	assert.True(t, decision.Allowed)

	cases := map[string]func(Scope, *model.Operation, []RefLine) (Scope, *model.Operation, []RefLine){
		"pending primary": func(scope Scope, op *model.Operation, lines []RefLine) (Scope, *model.Operation, []RefLine) {
			op.EffectState = model.EffectPending
			return scope, op, lines
		},
		"missing receipt": func(scope Scope, op *model.Operation, lines []RefLine) (Scope, *model.Operation, []RefLine) {
			op.Receipt = ""
			return scope, op, lines
		},
		"missing phase": func(scope Scope, op *model.Operation, lines []RefLine) (Scope, *model.Operation, []RefLine) {
			scope.CompletionPhase = ""
			return scope, op, lines
		},
		"wrong ref": func(scope Scope, op *model.Operation, lines []RefLine) (Scope, *model.Operation, []RefLine) {
			lines[0].Ref = "refs/heads/feature"
			return scope, op, lines
		},
		"wrong new oid": func(scope Scope, op *model.Operation, lines []RefLine) (Scope, *model.Operation, []RefLine) {
			lines[0].New = strings.Repeat("c", 40)
			return scope, op, lines
		},
		"unexpected old oid": func(scope Scope, op *model.Operation, lines []RefLine) (Scope, *model.Operation, []RefLine) {
			lines[0].Old = strings.Repeat("d", 40)
			return scope, op, lines
		},
		"second line": func(scope Scope, op *model.Operation, lines []RefLine) (Scope, *model.Operation, []RefLine) {
			return scope, op, append(lines, lines[0])
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			freshOp := *op
			freshLines := []RefLine{{Old: zero, New: head, Ref: "refs/pull/3/head"}}
			mutScope, mutOp, mutLines := mutate(scope, &freshOp, freshLines)
			decision := svc.checkPRCreateCompletion(mutScope, mutOp, mutLines)
			assert.False(t, decision.Allowed)
			assert.NotEmpty(t, decision.Reason)
		})
	}
}
