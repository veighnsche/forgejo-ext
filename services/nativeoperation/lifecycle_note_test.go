// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"strings"
	"testing"
	"time"

	model "forgejo.org/models/nativeoperation"

	"github.com/stretchr/testify/require"
)

func TestParseLifecycleNotePayloadAcceptsExactInput(t *testing.T) {
	intent, err := parseLifecycleNotePayload([]byte(`{"issue_id":12,"content":"Factory completed the work.","merge_ref":"pr/88@abc"}`), 7)
	require.NoError(t, err)
	require.Equal(t, int64(12), intent.IssueID)
	require.Equal(t, "pr/88@abc", intent.MergeRef)
}

func TestParseLifecycleNotePayloadRejects(t *testing.T) {
	cases := map[string]string{
		"empty":      ``,
		"unknown":    `{"issue_id":12,"content":"x","merge_ref":"m","attachments":["a"]}`,
		"no issue":   `{"content":"x","merge_ref":"m"}`,
		"no content": `{"issue_id":12,"merge_ref":"m"}`,
		"blank":      `{"issue_id":12,"content":"  ","merge_ref":"m"}`,
		"no ref":     `{"issue_id":12,"content":"x"}`,
		"too long":   `{"issue_id":12,"content":"` + strings.Repeat("x", MaxLifecycleNoteChars+1) + `","merge_ref":"m"}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseLifecycleNotePayload([]byte(payload), 7)
			require.ErrorIs(t, err, ErrInvalidIntent)
		})
	}
}

func TestValidateIntentAcceptsLifecycleNote(t *testing.T) {
	now := time.Now().Unix()
	intent, err := ValidateIntent("op-lifecycle-n1", 2, 1, model.KindLifecycleNote, "rev-1", 1, now+300, []byte(`{"issue_id":12,"content":"done","merge_ref":"m"}`), now)
	require.NoError(t, err)
	require.NotNil(t, intent.LifecycleNote)
	require.NotEmpty(t, intent.Digest)

	_, err = ValidateIntent("op-lifecycle-n2", 2, 1, model.KindLifecycleNote, "rev-1", 1, now+300, []byte(`{"issue_id":12,"content":"done"}`), now)
	require.ErrorIs(t, err, ErrInvalidIntent)
}
