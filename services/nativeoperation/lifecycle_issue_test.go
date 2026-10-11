// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"testing"
	"time"

	model "forgejo.org/models/nativeoperation"

	"github.com/stretchr/testify/require"
)

func TestParseLifecycleIssuePayloadAcceptsExactInput(t *testing.T) {
	intent, err := parseLifecycleIssuePayload([]byte(`{"issue_id":42,"label_id":9,"assignee_id":15}`), 7)
	require.NoError(t, err)
	require.Equal(t, int64(42), intent.IssueID)
	require.Equal(t, int64(9), intent.LabelID)
	require.Equal(t, int64(15), intent.AssigneeID)

	labelOnly, err := parseLifecycleIssuePayload([]byte(`{"issue_id":42,"label_id":9}`), 7)
	require.NoError(t, err)
	require.Zero(t, labelOnly.AssigneeID)
}

func TestParseLifecycleIssuePayloadRejects(t *testing.T) {
	cases := map[string]string{
		"empty":        ``,
		"not json":     `{oops`,
		"unknown":      `{"issue_id":42,"label_id":9,"milestone_id":3}`,
		"trailing":     `{"issue_id":42,"label_id":9} {}`,
		"no issue":     `{"label_id":9}`,
		"no effect":    `{"issue_id":42}`,
		"zero effect":  `{"issue_id":42,"label_id":0,"assignee_id":0}`,
		"negative":     `{"issue_id":42,"label_id":-1}`,
		"negative id":  `{"issue_id":-5,"label_id":9}`,
		"string types": `{"issue_id":"42","label_id":9}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseLifecycleIssuePayload([]byte(payload), 7)
			require.ErrorIs(t, err, ErrInvalidIntent)
		})
	}
	_, err := parseLifecycleIssuePayload([]byte(`{"issue_id":42,"label_id":9}`), 0)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestValidateIntentAcceptsLifecycleIssue(t *testing.T) {
	now := time.Now().Unix()
	intent, err := ValidateIntent("op-lifecycle-1", 2, 1, model.KindLifecycleIssue, "rev-1", 1, now+300, []byte(`{"issue_id":42,"assignee_id":15}`), now)
	require.NoError(t, err)
	require.NotNil(t, intent.LifecycleIssue)
	require.Equal(t, int64(42), intent.LifecycleIssue.IssueID)
	require.NotEmpty(t, intent.Digest)
	require.NotEmpty(t, intent.Canonical)

	_, err = ValidateIntent("op-lifecycle-2", 2, 1, model.KindLifecycleIssue, "rev-1", 1, now+300, []byte(`{"issue_id":42}`), now)
	require.ErrorIs(t, err, ErrInvalidIntent)
}
