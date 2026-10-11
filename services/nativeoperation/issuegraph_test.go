// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later
package nativeoperation

import (
	"context"
	"encoding/json"
	sdk "forgejo.org/extension-sdk"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	authmodel "forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	model "forgejo.org/models/nativeoperation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	execcontext "forgejo.org/modules/nativeoperation"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func graphPayload() sdk.IssueGraphPayload {
	return sdk.IssueGraphPayload{
		Creates:      []sdk.IssueGraphCreate{{Key: "first", Title: "First objective", Body: "First outcome"}, {Key: "second", Title: "Second objective", Body: "Second outcome"}},
		Dependencies: []sdk.IssueGraphDependency{{Issue: sdk.IssueGraphRef{Key: "second"}, Prerequisite: sdk.IssueGraphRef{Key: "first"}}},
	}
}
func graphIntent(t *testing.T, id string, p sdk.IssueGraphPayload) *ValidIntent {
	t.Helper()
	r, err := model.ReadReservation(t.Context())
	require.NoError(t, err)
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	now := time.Now().Unix()
	intent, err := ValidateIntent(id, 2, 1, model.KindIssueGraph, "accepted-revision", r.Revision, now+300, raw, now)
	require.NoError(t, err)
	return intent
}
func graphDecision(t *testing.T) authmodel.SubmissionDecision {
	t.Helper()
	ctx := t.Context()
	_, configErr := db.GetEngine(ctx).Exec(`UPDATE repo_unit SET config=? WHERE repo_id=1 AND type=2`, `{"EnableDependencies":true}`)
	require.NoError(t, configErr)
	token := unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: 3})
	token.Scope = auth_model.AccessTokenScopeWriteRepository
	token.ResourceAllRepos = true
	_, err := db.GetEngine(ctx).ID(token.ID).Cols("scope", "resource_all_repos").Update(token)
	require.NoError(t, err)
	_, err = authmodel.EnrollBinding(ctx, operationTestInstallation, token.ID, 2, 1, model.KindIssueGraph)
	require.NoError(t, err)
	return authmodel.SubmissionDecision{TokenID: token.ID, ActorID: 2, CredentialFingerprint: authmodel.CredentialFingerprint(token.TokenHash, token.TokenSalt)}
}
func TestParseIssueGraph(t *testing.T) {
	raw, err := json.Marshal(graphPayload())
	require.NoError(t, err)
	_, err = parseIssueGraphPayload(raw)
	require.NoError(t, err)
	_, err = parseIssueGraphPayload(append(raw, []byte(`{}`)...))
	require.ErrorIs(t, err, ErrInvalidIntent)
	_, err = parseIssueGraphPayload([]byte(`{"creates":[{"key":"first","title":"Title","body":"Body","close":true}]}`))
	require.ErrorIs(t, err, ErrInvalidIntent)
}
func TestIssueGraphSubmitReplayEditAndDependencies(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	decision := graphDecision(t)
	svc := NewService()
	ctx := t.Context()
	intent := graphIntent(t, "graph-create", graphPayload())
	record, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, record.EffectState, record.ReasonCode)
	var receipt sdk.IssueGraphReceipt
	require.NoError(t, json.Unmarshal(record.Receipt, &receipt))
	require.Len(t, receipt.Issues, 2)
	require.Len(t, receipt.Dependencies, 1)
	first := receipt.Issues[0]
	second := receipt.Issues[1]
	require.Equal(t, int64(2), receipt.ActorID)
	issue, err := issues_model.GetIssueByID(ctx, first.IssueID)
	require.NoError(t, err)
	require.Equal(t, int64(2), issue.PosterID)
	replay, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, record.Receipt, replay.Receipt)
	p := sdk.IssueGraphPayload{Edits: []sdk.IssueGraphEdit{{IssueID: issue.ID, ExpectedTitle: issue.Title, ExpectedBody: issue.Content, ExpectedContentVersion: issue.ContentVersion, Title: "Revised objective", Body: "Revised outcome"}}, Dependencies: []sdk.IssueGraphDependency{{Issue: sdk.IssueGraphRef{IssueID: second.IssueID}, Prerequisite: sdk.IssueGraphRef{IssueID: first.IssueID}, Remove: true}}}
	edited, err := svc.Submit(ctx, decision, operationTestInstallation, graphIntent(t, "graph-edit", p))
	require.NoError(t, err)
	require.Equal(t, model.EffectCommitted, edited.EffectState)
	issue, err = issues_model.GetIssueByID(ctx, first.IssueID)
	require.NoError(t, err)
	require.Equal(t, "Revised objective", issue.Title)
	require.Equal(t, 1, issue.ContentVersion)
	require.False(t, issue.IsClosed)
	history, err := issues_model.HasIssueContentHistory(ctx, issue.ID, 0)
	require.NoError(t, err)
	require.True(t, history)
	edges, err := issues_model.ListIssueDependencies(ctx, second.IssueID)
	require.NoError(t, err)
	require.Empty(t, edges)
}
func TestIssueGraphCycleRollsBackWholePrimary(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	decision := graphDecision(t)
	svc := NewService()
	ctx := t.Context()
	p := graphPayload()
	p.Creates = append(p.Creates, sdk.IssueGraphCreate{Key: "third", Title: "Third", Body: "Outcome"})
	p.Dependencies = append(p.Dependencies, sdk.IssueGraphDependency{Issue: sdk.IssueGraphRef{Key: "first"}, Prerequisite: sdk.IssueGraphRef{Key: "third"}}, sdk.IssueGraphDependency{Issue: sdk.IssueGraphRef{Key: "third"}, Prerequisite: sdk.IssueGraphRef{Key: "second"}})
	before, err := db.GetEngine(ctx).Where("repo_id=?", 1).Count(new(issues_model.Issue))
	require.NoError(t, err)
	record, err := svc.Submit(ctx, decision, operationTestInstallation, graphIntent(t, "graph-cycle", p))
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, record.EffectState)
	after, err := db.GetEngine(ctx).Where("repo_id=?", 1).Count(new(issues_model.Issue))
	require.NoError(t, err)
	require.Equal(t, before, after)
}
func TestIssueGraphPrimaryCancellationAndRecovery(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "cancelled"}[cancel], func(t *testing.T) {
			unittest.PrepareTestEnv(t)
			useIsolatedAppData(t)
			ctx := t.Context()
			p := graphPayload()
			intent := graphIntent(t, "graph-primary", p)
			op := &model.Operation{InstallationID: operationTestInstallation, OperationID: intent.OperationID, Kind: intent.Kind, ActorID: 2, RepositoryID: 1, NotAfter: intent.NotAfter, ExpectedNativeRevision: intent.ExpectedNativeRevision, IntentDigest: intent.Digest, Intent: intent.Canonical}
			owner := conditionalOwner(operationTestInstallation, intent.OperationID)
			scope := Scope{Kind: model.OwnerConditional, RepositoryID: 1}
			encoded, err := encodeScope(scope)
			require.NoError(t, err)
			held, err := model.ClaimConditional(ctx, op, owner, encoded, "verifier")
			require.NoError(t, err)
			owned := execcontext.NewContext(ctx, &execcontext.Execution{Owner: owner, Generation: held.Generation})
			repo, err := repo_model.GetRepositoryByID(ctx, 1)
			require.NoError(t, err)
			actor, err := user_model.GetUserByID(ctx, 2)
			require.NoError(t, err)
			if cancel {
				_, err = model.RevokeOperation(ctx, operationTestInstallation, intent.OperationID)
				require.NoError(t, err)
			}
			called := false
			committed, err := model.CommitIssueGraphPrimary(owned, operationTestInstallation, intent.OperationID, owner, time.Now().Unix(), func(tx context.Context) (string, error) {
				called = true
				receipt, _, err := applyIssueGraph(tx, repo, actor, &p)
				if err != nil {
					return "", err
				}
				data, err := json.Marshal(receipt)
				return string(data), err
			})
			if cancel {
				require.ErrorIs(t, err, model.ErrAdmissionLost)
				require.False(t, called)
			} else {
				require.NoError(t, err)
				require.Equal(t, model.EffectCommitted, committed.EffectState)
			}
			inhibitDomain(t)
			assessment, err := NewService().Recover(ctx, owner, held.Generation)
			require.NoError(t, err)
			require.Equal(t, RecoveryReleased, assessment.Verdict)
			recorded, err := model.LookupOperation(ctx, operationTestInstallation, intent.OperationID)
			require.NoError(t, err)
			if cancel {
				require.Equal(t, model.EffectNotCommitted, recorded.EffectState)
			} else {
				require.Equal(t, model.CompletionNeedsIntervention, recorded.Completion)
			}
		})
	}
}

func TestIssueGraphStaleInputsAndAuthorityRefuse(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	decision := graphDecision(t)
	svc := NewService()
	ctx := t.Context()
	stale := graphIntent(t, "graph-stale-revision", graphPayload())
	// An intervening ordinary native writer advances the same reservation.
	owner := "ord:collaboration/issue/new/1/fixture"
	held, err := model.ClaimOrdinary(ctx, owner, `{"kind":"ordinary","family":"collaboration","repository_id":1}`, "verifier")
	require.NoError(t, err)
	require.NoError(t, model.ReleaseExactOwner(ctx, owner, held.Generation))
	record, err := svc.Submit(ctx, decision, operationTestInstallation, stale)
	require.NoError(t, err)
	require.Equal(t, model.ReasonStaleNativeRevision, record.ReasonCode)
	p := graphPayload()
	issue, err := issues_model.GetIssueByID(ctx, 1)
	require.NoError(t, err)
	p.Edits = []sdk.IssueGraphEdit{{IssueID: 1, ExpectedTitle: issue.Title, ExpectedBody: "stale body", Title: "Changed", Body: "New body"}}
	before, err := db.GetEngine(ctx).Where("repo_id=?", 1).Count(new(issues_model.Issue))
	require.NoError(t, err)
	record, err = svc.Submit(ctx, decision, operationTestInstallation, graphIntent(t, "graph-stale-text", p))
	require.NoError(t, err)
	require.Equal(t, model.EffectNotCommitted, record.EffectState)
	after, err := db.GetEngine(ctx).Where("repo_id=?", 1).Count(new(issues_model.Issue))
	require.NoError(t, err)
	require.Equal(t, before, after)
	bad := decision
	bad.CredentialFingerprint = "revoked-generation"
	record, err = svc.Submit(ctx, bad, operationTestInstallation, graphIntent(t, "graph-authority-loss", graphPayload()))
	require.NoError(t, err)
	require.Equal(t, model.ReasonAuthorityLost, record.ReasonCode)
}
