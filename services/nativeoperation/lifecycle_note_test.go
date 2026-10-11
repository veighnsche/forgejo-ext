// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	authmodel "forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func lifecycleNoteDecision(t *testing.T) authmodel.SubmissionDecision {
	t.Helper()
	ctx := t.Context()
	token := unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: 3})
	token.Scope = auth_model.AccessTokenScopeWriteRepository
	token.ResourceAllRepos = true
	_, err := db.GetEngine(ctx).ID(token.ID).Cols("scope", "resource_all_repos").Update(token)
	require.NoError(t, err)
	_, err = authmodel.EnrollBinding(ctx, operationTestInstallation, token.ID, 2, 1, model.KindLifecycleNote)
	require.NoError(t, err)
	return authmodel.SubmissionDecision{TokenID: token.ID, ActorID: 2, CredentialFingerprint: authmodel.CredentialFingerprint(token.TokenHash, token.TokenSalt)}
}

func lifecycleNoteIntent(t *testing.T, id string, payload []byte) *ValidIntent {
	t.Helper()
	r, err := model.ReadReservation(t.Context())
	require.NoError(t, err)
	now := time.Now().Unix()
	intent, err := ValidateIntent(id, 2, 1, model.KindLifecycleNote, "rev-1", r.Revision, now+300, payload, now)
	require.NoError(t, err)
	return intent
}

func TestLifecycleNoteSubmitRoundTrip(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	svc := NewService()
	decision := lifecycleNoteDecision(t)

	intent := lifecycleNoteIntent(t, "op-life-note-1", []byte(`{"issue_id":5,"content":"Factory completed the accepted work.","merge_ref":"pr/2@abc"}`))
	record, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, record.EffectState, record.ReasonCode)
	var receipt LifecycleNoteReceipt
	require.NoError(t, json.Unmarshal(record.Receipt, &receipt))
	require.Equal(t, int64(5), receipt.IssueID)
	require.Positive(t, receipt.CommentID)
	comment, err := issues_model.GetCommentByID(ctx, receipt.CommentID)
	require.NoError(t, err)
	require.Equal(t, "Factory completed the accepted work.", comment.Content)

	replay, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, record.Receipt, replay.Receipt)
	count, err := issues_model.CountComments(ctx, &issues_model.FindCommentsOptions{IssueID: 5})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	lookup, err := svc.Get(ctx, operationTestInstallation, "op-life-note-1")
	require.NoError(t, err)
	require.NotNil(t, lookup.Record)
	require.Equal(t, model.EffectCommitted, lookup.Status)

	open := lifecycleNoteIntent(t, "op-life-note-2", []byte(`{"issue_id":1,"content":"Too early.","merge_ref":"pr/2@abc"}`))
	refused, err := svc.Submit(ctx, decision, operationTestInstallation, open)
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, refused.EffectState)
}

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
