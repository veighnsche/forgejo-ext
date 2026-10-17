// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"strings"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	extensionauth_model "forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	nativeoperation_model "forgejo.org/models/nativeoperation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"

	"github.com/stretchr/testify/require"
)

// FW17 conditional thread-resolution proof: exact digest binding through
// CanMarkConversation/MarkConversation, refusal on missing binding, wrong
// actor, reply targets, deleted roots and changed content, with exact
// replay and readback.

func resolveDecision(t *testing.T, actorID, tokenID int64) extensionauth_model.SubmissionDecision {
	t.Helper()
	ctx := t.Context()
	token := unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: tokenID})
	token.Scope = auth_model.AccessTokenScopeWriteRepository
	token.ResourceAllRepos = true
	_, err := db.GetEngine(ctx).ID(token.ID).Cols("scope", "resource_all_repos").Update(token)
	require.NoError(t, err)
	_, err = extensionauth_model.EnrollBinding(ctx, operationTestInstallation, token.ID, actorID, 1, nativeoperation_model.KindThreadResolve)
	require.NoError(t, err)
	return extensionauth_model.SubmissionDecision{TokenID: token.ID, ActorID: actorID, CredentialFingerprint: extensionauth_model.CredentialFingerprint(token.TokenHash, token.TokenSalt)}
}

func resolveIntent(t *testing.T, id string, actorID int64, payload sdk.ThreadResolvePayload) *ValidIntent {
	t.Helper()
	r, err := nativeoperation_model.ReadReservation(t.Context())
	require.NoError(t, err)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	now := time.Now().Unix()
	intent, err := ValidateIntent(id, actorID, 1, nativeoperation_model.KindThreadResolve, "accepted-revision", r.Revision, now+300, raw, now)
	require.NoError(t, err)
	require.NotNil(t, intent.ThreadResolve)
	return intent
}

func resolveTips(t *testing.T, svc *Service) (head, base string) {
	t.Helper()
	ctx := t.Context()
	repository := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	head, err := svc.readRef(ctx, repository.RepoPath(), git.BranchPrefix+"branch2")
	require.NoError(t, err)
	base, err = svc.readRef(ctx, repository.RepoPath(), git.BranchPrefix+"master")
	require.NoError(t, err)
	require.NotEmpty(t, head)
	require.NotEmpty(t, base)
	return head, base
}

func resolveThread(t *testing.T, svc *Service, commit string) (*issues_model.Comment, *issues_model.Comment, string) {
	t.Helper()
	issue, doer := threadFixture(t)
	review := insertThreadReview(t, issue.ID, 2, issues_model.ReviewTypeComment)
	root := insertThreadComment(t, issue, doer, review.ID, "resolve.go", 12, commit, "needs a guard")
	reply := insertThreadComment(t, issue, doer, review.ID, "resolve.go", 12, commit, "guard added")
	freshRoot := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: root.ID})
	freshReply := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: reply.ID})
	return freshRoot, freshReply, expectedThreadDigest(t, freshRoot, freshReply)
}

func resolvePayload(head, base, digest string, rootID int64) sdk.ThreadResolvePayload {
	return sdk.ThreadResolvePayload{
		HeadRef:            git.BranchPrefix + "branch2",
		BaseRef:            git.BranchPrefix + "master",
		ExpectedHeadOID:    head,
		ExpectedBaseOID:    base,
		ConversationDigest: digest,
		PullRequestNumber:  3,
		HeadRepositoryID:   1,
		RootCommentID:      rootID,
		Resolved:           true,
	}
}

func TestParseThreadResolvePayloadRoundTrip(t *testing.T) {
	head := strings.Repeat("1", 40)
	base := strings.Repeat("2", 40)
	payload := resolvePayload(head, base, strings.Repeat("a", 64), 11)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	parsed, err := parseThreadResolvePayload(raw, 1)
	require.NoError(t, err)
	require.Equal(t, int64(3), parsed.PullRequestNumber)
	unscoped, err := parseThreadResolvePayload(raw, 9)
	require.ErrorIs(t, err, ErrInvalidIntent, "fork sources refuse: %v", unscoped)
	_, err = parseThreadResolvePayload(append(raw, []byte(` `)...), 1)
	require.NoError(t, err, " trailing whitespace is not a second value")
	trailing, err := json.Marshal(payload)
	require.NoError(t, err)
	_, err = parseThreadResolvePayload(append(trailing, []byte(`{}`)...), 1)
	require.ErrorIs(t, err, ErrInvalidIntent)
	unknown, err := json.Marshal(struct {
		sdk.ThreadResolvePayload
		Extra string `json:"extra"`
	}{ThreadResolvePayload: payload, Extra: "nope"})
	require.NoError(t, err)
	_, err = parseThreadResolvePayload(unknown, 1)
	require.ErrorIs(t, err, ErrInvalidIntent)
	_, err = parseThreadResolvePayload(nil, 1)
	require.ErrorIs(t, err, ErrInvalidIntent)
}

func TestThreadResolveCommitsReceiptAndReplays(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	svc := NewService()
	decision := resolveDecision(t, 2, 3)

	head, base := resolveTips(t, svc)
	root, _, digest := resolveThread(t, svc, head)

	intent := resolveIntent(t, "thread-resolve-ok", 2, resolvePayload(head, base, digest, root.ID))
	record, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, nativeoperation_model.EffectCommitted, record.EffectState, record.ReasonCode)
	var receipt sdk.ThreadResolveReceipt
	require.NoError(t, json.Unmarshal(record.Receipt, &receipt))
	require.Equal(t, int64(3), receipt.PullRequestNumber)
	require.Equal(t, root.ID, receipt.RootCommentID)
	require.True(t, receipt.Resolved)
	require.Equal(t, strings.ToLower(digest), receipt.ConversationDigest)

	resolved := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: root.ID})
	require.Equal(t, int64(2), resolved.ResolveDoerID)

	// Exact replay returns the recorded receipt without relaunching.
	replay, err := svc.Submit(ctx, decision, operationTestInstallation, intent)
	require.NoError(t, err)
	require.Equal(t, record.Receipt, replay.Receipt)

	// Readback exposes the recorded resolve effect.
	lookup, err := svc.Get(ctx, operationTestInstallation, "thread-resolve-ok")
	require.NoError(t, err)
	require.NotNil(t, lookup.Record)
	require.Equal(t, record.Receipt, lookup.Record.Receipt)

	// Stable re-resolution: resolving the resolved root with its current
	// digest commits without ownership change. The receipt carries the
	// new digest because resolution flips member resolver state from
	// open to resolved, which the digest binds.
	members := threadMembersForDigest(t, ctx, root)
	current := expectedThreadDigest(t, members...)
	require.NotEqual(t, digest, current, "resolution changes the bound digest")
	stable, err := svc.Submit(ctx, decision, operationTestInstallation, resolveIntent(t, "thread-resolve-stable", 2, resolvePayload(head, base, current, root.ID)))
	require.NoError(t, err)
	require.Equal(t, nativeoperation_model.EffectCommitted, stable.EffectState, stable.ReasonCode)
	var stableReceipt sdk.ThreadResolveReceipt
	require.NoError(t, json.Unmarshal(stable.Receipt, &stableReceipt))
	require.Equal(t, strings.ToLower(current), stableReceipt.ConversationDigest)
	restable := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: root.ID})
	require.Equal(t, int64(2), restable.ResolveDoerID, "re-resolution changes no ownership")
}

// threadMembersForDigest reloads one conversation in native order for an
// independent digest recomputation.
func threadMembersForDigest(t *testing.T, ctx context.Context, root *issues_model.Comment) []*issues_model.Comment {
	t.Helper()
	var members []*issues_model.Comment
	require.NoError(t, db.GetEngine(ctx).Where("issue_id = ?", root.IssueID).
		And("review_id = ?", root.ReviewID).
		And("tree_path = ?", root.TreePath).
		And("line = ?", root.Line).
		Asc("created_unix").Asc("id").
		Find(&members))
	require.NotEmpty(t, members)
	return members
}

func TestThreadResolveRefusesStaleDigest(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	svc := NewService()
	decision := resolveDecision(t, 2, 3)

	head, base := resolveTips(t, svc)
	root, _, digest := resolveThread(t, svc, head)

	// A concurrent reply after the snapshot invalidates the digest.
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	require.NoError(t, issue.LoadRepo(ctx))
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	_, err := issues_model.CreateComment(ctx, &issues_model.CreateCommentOptions{
		Type: issues_model.CommentTypeCode, Doer: doer, Repo: issue.Repo, Issue: issue,
		ReviewID: root.ReviewID, TreePath: "resolve.go", LineNum: 12, CommitSHA: head, Content: "concurrent reply",
	})
	require.NoError(t, err)

	record, err := svc.Submit(ctx, decision, operationTestInstallation, resolveIntent(t, "thread-resolve-stale", 2, resolvePayload(head, base, digest, root.ID)))
	require.NoError(t, err)
	require.Equal(t, nativeoperation_model.EffectNotCommitted, record.EffectState)
	require.Equal(t, nativeoperation_model.ReasonStaleConversation, record.ReasonCode)
}

func TestThreadResolveRefusesReplyTarget(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	svc := NewService()
	decision := resolveDecision(t, 2, 3)

	head, base := resolveTips(t, svc)
	root, reply, digest := resolveThread(t, svc, head)

	record, err := svc.Submit(ctx, decision, operationTestInstallation, resolveIntent(t, "thread-resolve-reply", 2, resolvePayload(head, base, digest, reply.ID)))
	require.NoError(t, err)
	require.Equal(t, nativeoperation_model.EffectNotCommitted, record.EffectState)
	require.Equal(t, nativeoperation_model.ReasonPRMismatch, record.ReasonCode)

	still := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: root.ID})
	require.Zero(t, still.ResolveDoerID, "refused resolution writes nothing")
}

func TestThreadResolveRefusesDeletedRoot(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	svc := NewService()
	decision := resolveDecision(t, 2, 3)

	head, base := resolveTips(t, svc)
	root, _, digest := resolveThread(t, svc, head)

	_, err := db.GetEngine(ctx).ID(root.ID).Delete(new(issues_model.Comment))
	require.NoError(t, err)

	record, err := svc.Submit(ctx, decision, operationTestInstallation, resolveIntent(t, "thread-resolve-deleted", 2, resolvePayload(head, base, digest, root.ID)))
	require.NoError(t, err)
	require.Equal(t, nativeoperation_model.EffectNotCommitted, record.EffectState)
	require.Equal(t, nativeoperation_model.ReasonPRMismatch, record.ReasonCode)
}

func TestThreadResolveRefusesWrongActor(t *testing.T) {
	unittest.PrepareTestEnv(t)
	useIsolatedAppData(t)
	ctx := t.Context()
	svc := NewService()

	// Actor 5 holds no resolve authority on repo1; enrollment uses a fresh
	// write-scoped token so execution reaches the native permission
	// check instead of failing at admission.
	secret := "thread-resolve-outsider-secret"
	salt := "outsider-salt-01"
	token := &auth_model.AccessToken{
		UID: 5, Name: "resolve-outsider",
		TokenHash: auth_model.HashToken(secret, salt), TokenSalt: salt, TokenLastEight: "secret!!",
		Scope: auth_model.AccessTokenScopeWriteRepository, ResourceAllRepos: true,
		CreatedUnix: 1700000000, UpdatedUnix: 1700000000,
	}
	unittest.AssertSuccessfulInsert(t, token)
	_, err := extensionauth_model.EnrollBinding(ctx, operationTestInstallation, token.ID, 5, 1, nativeoperation_model.KindThreadResolve)
	require.NoError(t, err)
	decision := extensionauth_model.SubmissionDecision{TokenID: token.ID, ActorID: 5, CredentialFingerprint: extensionauth_model.CredentialFingerprint(token.TokenHash, token.TokenSalt)}

	head, base := resolveTips(t, svc)
	root, _, digest := resolveThread(t, svc, head)

	record, err := svc.Submit(ctx, decision, operationTestInstallation, resolveIntent(t, "thread-resolve-outsider", 5, resolvePayload(head, base, digest, root.ID)))
	require.NoError(t, err)
	require.Equal(t, nativeoperation_model.EffectNotCommitted, record.EffectState)
	require.Equal(t, nativeoperation_model.ReasonAuthorityLost, record.ReasonCode)
}
