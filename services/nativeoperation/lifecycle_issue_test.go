// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"encoding/json"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	authmodel "forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/require"
)

func lifecycleIssueDecision(t *testing.T, repo int64, kind string) authmodel.SubmissionDecision {
	t.Helper()
	ctx := t.Context()
	token := unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: 3})
	token.Scope = auth_model.AccessTokenScopeWriteRepository
	token.ResourceAllRepos = true
	_, err := db.GetEngine(ctx).ID(token.ID).Cols("scope", "resource_all_repos").Update(token)
	require.NoError(t, err)
	_, err = authmodel.EnrollBinding(ctx, operationTestInstallation, token.ID, 2, repo, kind)
	require.NoError(t, err)
	return authmodel.SubmissionDecision{TokenID: token.ID, ActorID: 2, CredentialFingerprint: authmodel.CredentialFingerprint(token.TokenHash, token.TokenSalt)}
}

func lifecycleIssueIntent(t *testing.T, id string, payload []byte) *ValidIntent {
	t.Helper()
	r, err := model.ReadReservation(t.Context())
	require.NoError(t, err)
	now := time.Now().Unix()
	intent, err := ValidateIntent(id, 2, 3, model.KindLifecycleIssue, "rev-1", r.Revision, now+300, payload, now)
	require.NoError(t, err)
	return intent
}

func TestLifecycleIssueSubmitRoundTrip(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	svc := NewService()
	decision := lifecycleIssueDecision(t, 3, model.KindLifecycleIssue)

	intent := lifecycleIssueIntent(t, "op-life-issue-1", []byte(`{"issue_id":6,"label_id":3,"assignee_id":4}`))
	record, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, record.EffectState, record.ReasonCode)
	var receipt LifecycleIssueReceipt
	require.NoError(t, json.Unmarshal(record.Receipt, &receipt))
	require.Equal(t, int64(6), receipt.IssueID)
	require.True(t, receipt.LabelAdded)
	require.True(t, receipt.AssigneeAdded)
	require.True(t, issues_model.HasIssueLabel(ctx, 6, 3))
	assignee, err := user_model.GetUserByID(ctx, 4)
	require.NoError(t, err)
	issue, err := issues_model.GetIssueByID(ctx, 6)
	require.NoError(t, err)
	assigned, err := issues_model.IsUserAssignedToIssue(ctx, issue, assignee)
	require.NoError(t, err)
	require.True(t, assigned)

	replay, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, record.Receipt, replay.Receipt)

	lookup, err := svc.Get(ctx, operationTestInstallation, "op-life-issue-1")
	require.NoError(t, err)
	require.NotNil(t, lookup.Record)
	require.Equal(t, model.EffectCommitted, lookup.Status)

	changed := lifecycleIssueIntent(t, "op-life-issue-1", []byte(`{"issue_id":6,"label_id":3}`))
	_, err = svc.Submit(ctx, decision, operationTestInstallation, changed)
	require.ErrorIs(t, err, ErrIntentConflict)

	unknownLabel := lifecycleIssueIntent(t, "op-life-issue-2", []byte(`{"issue_id":6,"label_id":999}`))
	refused, err := svc.Submit(ctx, decision, operationTestInstallation, unknownLabel)
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, refused.EffectState)

	ineligible := lifecycleIssueIntent(t, "op-life-issue-3", []byte(`{"issue_id":6,"assignee_id":29}`))
	denied, err := svc.Submit(ctx, decision, operationTestInstallation, ineligible)
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, denied.EffectState)

	cancelled, err := svc.Cancel(ctx, operationTestInstallation, "op-life-issue-4")
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, cancelled.Outcome)
	preempted := lifecycleIssueIntent(t, "op-life-issue-4", []byte(`{"issue_id":6,"label_id":3}`))
	_, err = svc.Submit(ctx, decision, operationTestInstallation, preempted)
	require.ErrorIs(t, err, ErrCancelledBeforeSubmit)
}

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
