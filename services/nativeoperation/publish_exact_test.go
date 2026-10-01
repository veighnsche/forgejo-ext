// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	publishOldSHA = "65f1bf27bc3bf70f64657658635e66094edbcb4d"
	publishNewSHA = "985f0301dba5e7b34be866819cd15ad3d8f508ee"
	publishBase   = "27566bd78f530c040ef74d0bbb6cfd9a87db7a39"
	publishZero40 = "0000000000000000000000000000000000000000"
)

func publishPayload(extra string) string {
	return `{"ref":"refs/heads/candidate-1","expected_old":"` + publishOldSHA +
		`","new_oid":"` + publishNewSHA + `","comparison_ref":"refs/heads/master",` +
		`"expected_comparison_oid":"` + publishBase + `"` + extra + `}`
}

func TestParsePublishPayload(t *testing.T) {
	intent, err := ParsePublishPayload([]byte(publishPayload("")))
	require.NoError(t, err)
	require.False(t, intent.IsCreation())
	assert.Equal(t, "refs/heads/candidate-1", intent.Ref)
	assert.Equal(t, publishNewSHA, intent.NewOID)
	assert.Nil(t, intent.Correction)

	creation, err := ParsePublishPayload([]byte(`{"ref":"refs/heads/fresh","expected_old":"absent",` +
		`"new_oid":"` + publishNewSHA + `","comparison_ref":"refs/heads/master",` +
		`"expected_comparison_oid":"` + publishBase + `"}`))
	require.NoError(t, err)
	require.True(t, creation.IsCreation())

	correction, err := ParsePublishPayload([]byte(publishPayload(`,"pull_request":{"number":7,"expected_author_id":2}`)))
	require.NoError(t, err)
	require.NotNil(t, correction.Correction)
	assert.Equal(t, int64(7), correction.Correction.Number)
	assert.Equal(t, int64(2), correction.Correction.ExpectedAuthorID)

	upper, err := ParsePublishPayload([]byte(publishPayload("")))
	require.NoError(t, err)
	assert.Equal(t, strings.ToLower(publishNewSHA), upper.NewOID)

	rejects := map[string]string{
		"empty":            ``,
		"not JSON":         `{`,
		"unknown field":    publishPayload(`,"extra":1`),
		"trailing data":    publishPayload("") + `{}`,
		"short ref":        strings.Replace(publishPayload(""), `"refs/heads/candidate-1"`, `"candidate-1"`, 1),
		"tag ref":          strings.Replace(publishPayload(""), `"refs/heads/candidate-1"`, `"refs/tags/v1"`, 1),
		"ref equals base":  strings.Replace(publishPayload(""), `"refs/heads/master"`, `"refs/heads/candidate-1"`, 1),
		"omitted old":      strings.Replace(publishPayload(""), `"expected_old":"`+publishOldSHA+`",`, ``, 1),
		"zero old":         strings.Replace(publishPayload(""), `"expected_old":"`+publishOldSHA+`"`, `"expected_old":"`+publishZero40+`"`, 1),
		"short old":        strings.Replace(publishPayload(""), `"expected_old":"`+publishOldSHA+`"`, `"expected_old":"65f1bf27"`, 1),
		"zero new":         strings.Replace(publishPayload(""), `"new_oid":"`+publishNewSHA+`"`, `"new_oid":"`+publishZero40+`"`, 1),
		"new equals old":   strings.Replace(publishPayload(""), `"new_oid":"`+publishNewSHA+`"`, `"new_oid":"`+publishOldSHA+`"`, 1),
		"zero base":        strings.Replace(publishPayload(""), `"expected_comparison_oid":"`+publishBase+`"`, `"expected_comparison_oid":"`+publishZero40+`"`, 1),
		"correction zero":  publishPayload(`,"pull_request":{"number":0,"expected_author_id":2}`),
		"creation correct": `{"ref":"refs/heads/fresh","expected_old":"absent","new_oid":"` + publishNewSHA + `","comparison_ref":"refs/heads/master","expected_comparison_oid":"` + publishBase + `","pull_request":{"number":7,"expected_author_id":2}}`,
	}
	for name, payload := range rejects {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePublishPayload([]byte(payload))
			require.ErrorIs(t, err, ErrInvalidPublishIntent)
		})
	}
}

func mustPublishIntent(t *testing.T, payload string) *PublishIntent {
	t.Helper()
	intent, err := ParsePublishPayload([]byte(payload))
	require.NoError(t, err)
	return intent
}

func TestVerifyPublishCommands(t *testing.T) {
	intent := mustPublishIntent(t, publishPayload(""))
	creation := mustPublishIntent(t, `{"ref":"refs/heads/fresh","expected_old":"absent",`+
		`"new_oid":"`+publishNewSHA+`","comparison_ref":"refs/heads/master",`+
		`"expected_comparison_oid":"`+publishBase+`"}`)

	require.NoError(t, VerifyPublishCommands(intent, publishZero40, []ReceiveLine{
		{Old: publishOldSHA, New: publishNewSHA, Ref: "refs/heads/candidate-1"},
	}))
	require.NoError(t, VerifyPublishCommands(creation, publishZero40, []ReceiveLine{
		{Old: publishZero40, New: publishNewSHA, Ref: "refs/heads/fresh"},
	}))

	cases := map[string]struct {
		intent *PublishIntent
		lines  []ReceiveLine
		reason string
	}{
		"nil intent":     {nil, []ReceiveLine{{Old: publishOldSHA, New: publishNewSHA, Ref: "refs/heads/candidate-1"}}, ExactPublishRefusedExtraCommand},
		"no commands":    {intent, nil, ExactPublishRefusedExtraCommand},
		"extra ref":      {intent, []ReceiveLine{{Old: publishOldSHA, New: publishNewSHA, Ref: "refs/heads/candidate-1"}, {Old: publishZero40, New: publishNewSHA, Ref: "refs/heads/other"}}, ExactPublishRefusedExtraCommand},
		"wrong ref":      {intent, []ReceiveLine{{Old: publishOldSHA, New: publishNewSHA, Ref: "refs/heads/other"}}, ExactPublishRefusedCommandRef},
		"wrong old":      {intent, []ReceiveLine{{Old: publishBase, New: publishNewSHA, Ref: "refs/heads/candidate-1"}}, ExactPublishRefusedCommandTuple},
		"wrong new":      {intent, []ReceiveLine{{Old: publishOldSHA, New: publishBase, Ref: "refs/heads/candidate-1"}}, ExactPublishRefusedCommandTuple},
		"creation old":   {creation, []ReceiveLine{{Old: publishOldSHA, New: publishNewSHA, Ref: "refs/heads/fresh"}}, ExactPublishRefusedCommandTuple},
		"deletion shape": {intent, []ReceiveLine{{Old: publishOldSHA, New: publishZero40, Ref: "refs/heads/candidate-1"}}, ExactPublishRefusedCommandTuple},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := VerifyPublishCommands(tc.intent, publishZero40, tc.lines)
			require.Error(t, err)
			var refused ErrExactPublishRefused
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, tc.reason, refused.Reason)
		})
	}
}

func TestVerifyPublishResult(t *testing.T) {
	intent := mustPublishIntent(t, publishPayload(""))
	require.NoError(t, VerifyPublishResult(intent, publishNewSHA, publishBase))
	require.NoError(t, VerifyPublishResult(intent, strings.ToUpper(publishNewSHA), strings.ToUpper(publishBase)))

	err := VerifyPublishResult(intent, publishOldSHA, publishBase)
	var refused ErrExactPublishRefused
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, ExactPublishRefusedResultMismatch, refused.Reason)

	err = VerifyPublishResult(intent, publishNewSHA, publishOldSHA)
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, ExactPublishRefusedCompareMoved, refused.Reason)

	err = VerifyPublishResult(nil, publishNewSHA, publishBase)
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, ExactPublishRefusedResultMismatch, refused.Reason)
}

func TestBuildPublishReceipt(t *testing.T) {
	intent := mustPublishIntent(t, publishPayload(""))
	target := &PublishTarget{
		Ref:           intent.Ref,
		OldOID:        intent.ExpectedOld,
		NewOID:        intent.NewOID,
		ComparisonRef: intent.ComparisonRef,
		ComparisonOID: intent.ExpectedComparisonOID,
	}
	receipt, err := BuildPublishReceipt(intent, target, 1, 2, publishNewSHA, publishBase)
	require.NoError(t, err)
	assert.Equal(t, int64(1), receipt.RepositoryID)
	assert.Equal(t, "refs/heads/candidate-1", receipt.Ref)
	assert.Equal(t, publishOldSHA, receipt.OldOID)
	assert.Equal(t, publishNewSHA, receipt.NewOID)
	assert.Equal(t, "refs/heads/master", receipt.ComparisonRef)
	assert.Equal(t, publishBase, receipt.ComparisonOID)
	assert.Equal(t, int64(2), receipt.ActorID)
	assert.Zero(t, receipt.PRID)

	_, err = BuildPublishReceipt(intent, target, 1, 2, publishOldSHA, publishBase)
	require.Error(t, err)
	var refused ErrExactPublishRefused
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, ExactPublishRefusedResultMismatch, refused.Reason)

	_, err = BuildPublishReceipt(intent, target, 0, 2, publishNewSHA, publishBase)
	require.Error(t, err)
	_, err = BuildPublishReceipt(intent, nil, 1, 2, publishNewSHA, publishBase)
	require.Error(t, err)
}
