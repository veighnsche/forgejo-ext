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

	"github.com/stretchr/testify/require"
)

func lifecycleReviewDecision(t *testing.T) authmodel.SubmissionDecision {
	t.Helper()
	ctx := t.Context()
	token := unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: 3})
	token.Scope = auth_model.AccessTokenScopeWriteRepository
	token.ResourceAllRepos = true
	_, err := db.GetEngine(ctx).ID(token.ID).Cols("scope", "resource_all_repos").Update(token)
	require.NoError(t, err)
	_, err = authmodel.EnrollBinding(ctx, operationTestInstallation, token.ID, 2, 1, model.KindLifecycleReview)
	require.NoError(t, err)
	return authmodel.SubmissionDecision{TokenID: token.ID, ActorID: 2, CredentialFingerprint: authmodel.CredentialFingerprint(token.TokenHash, token.TokenSalt)}
}

func lifecycleReviewIntent(t *testing.T, id string, payload []byte) *ValidIntent {
	t.Helper()
	r, err := model.ReadReservation(t.Context())
	require.NoError(t, err)
	now := time.Now().Unix()
	intent, err := ValidateIntent(id, 2, 1, model.KindLifecycleReview, "rev-1", r.Revision, now+300, payload, now)
	require.NoError(t, err)
	return intent
}

func TestLifecycleReviewSubmitRoundTrip(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	svc := NewService()
	decision := lifecycleReviewDecision(t)

	intent := lifecycleReviewIntent(t, "op-life-review-1", []byte(`{"issue_id":2,"reviewer_ids":[2]}`))
	record, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, record.EffectState, record.ReasonCode)
	var receipt LifecycleReviewReceipt
	require.NoError(t, json.Unmarshal(record.Receipt, &receipt))
	require.Equal(t, []int64{2}, receipt.Requested)
	require.Empty(t, receipt.AlreadyRequested)

	replay, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, record.Receipt, replay.Receipt)

	again := lifecycleReviewIntent(t, "op-life-review-2", []byte(`{"issue_id":2,"reviewer_ids":[2]}`))
	second, err := svc.Submit(ctx, decision, operationTestInstallation, again)
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, second.EffectState)
	var receipt2 LifecycleReviewReceipt
	require.NoError(t, json.Unmarshal(second.Receipt, &receipt2))
	require.Empty(t, receipt2.Requested)
	require.Equal(t, []int64{2}, receipt2.AlreadyRequested)

	// Repo 1 is public, so user 4 legitimately holds read access and
	// is an eligible reviewer; an unknown user cannot be requested.
	unknown := lifecycleReviewIntent(t, "op-life-review-3", []byte(`{"issue_id":2,"reviewer_ids":[9999]}`))
	denied, err := svc.Submit(ctx, decision, operationTestInstallation, unknown)
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, denied.EffectState)

	notPR := lifecycleReviewIntent(t, "op-life-review-4", []byte(`{"issue_id":1,"reviewer_ids":[2]}`))
	refused, err := svc.Submit(ctx, decision, operationTestInstallation, notPR)
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, refused.EffectState)

	_, err = issues_model.GetReviewByIssueIDAndUserID(ctx, 2, 2)
	require.NoError(t, err)
}

func TestParseLifecycleReviewPayloadAcceptsExactInput(t *testing.T) {
	intent, err := parseLifecycleReviewPayload([]byte(`{"issue_id":88,"reviewer_ids":[15,23]}`), 7)
	require.NoError(t, err)
	require.Equal(t, int64(88), intent.IssueID)
	require.Equal(t, []int64{15, 23}, intent.ReviewerIDs)
}

func TestParseLifecycleReviewPayloadRejects(t *testing.T) {
	cases := map[string]string{
		"empty":      ``,
		"unknown":    `{"issue_id":88,"reviewer_ids":[15],"team_ids":[3]}`,
		"no issue":   `{"reviewer_ids":[15]}`,
		"no reviews": `{"issue_id":88}`,
		"empty list": `{"issue_id":88,"reviewer_ids":[]}`,
		"too many":   `{"issue_id":88,"reviewer_ids":[1,2,3,4]}`,
		"duplicate":  `{"issue_id":88,"reviewer_ids":[15,15]}`,
		"zero id":    `{"issue_id":88,"reviewer_ids":[0]}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseLifecycleReviewPayload([]byte(payload), 7)
			require.ErrorIs(t, err, ErrInvalidIntent)
		})
	}
}

func TestValidateIntentAcceptsLifecycleReview(t *testing.T) {
	now := time.Now().Unix()
	intent, err := ValidateIntent("op-lifecycle-r1", 2, 1, model.KindLifecycleReview, "rev-1", 1, now+300, []byte(`{"issue_id":88,"reviewer_ids":[15]}`), now)
	require.NoError(t, err)
	require.NotNil(t, intent.LifecycleReview)
	require.NotEmpty(t, intent.Digest)

	_, err = ValidateIntent("op-lifecycle-r2", 2, 1, model.KindLifecycleReview, "rev-1", 1, now+300, []byte(`{"issue_id":88,"reviewer_ids":[1,2,3,4]}`), now)
	require.ErrorIs(t, err, ErrInvalidIntent)
}
