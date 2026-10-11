// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	webhook_model "forgejo.org/models/webhook"
	api "forgejo.org/modules/structs"
	webhook_module "forgejo.org/modules/webhook"

	"github.com/stretchr/testify/require"
)

func recoveryAuthority(t *testing.T, repository int64) (extensionauth.SnapshotReadDecision, sdk.CreationRecoveryRequest) {
	t.Helper()
	token := &auth_model.AccessToken{UID: 2, Name: "creation-recovery", Scope: "read:issue", ResourceAllRepos: true}
	require.NoError(t, auth_model.NewAccessToken(t.Context(), token))
	_, err := extensionauth.EnrollBinding(t.Context(), operationTestInstallation, token.ID, 2, repository, extensionauth.KindMerge)
	require.NoError(t, err)
	decision, err := extensionauth.VerifySnapshotRead(t.Context(), extensionauth.SnapshotReadRequest{
		InstallationID: operationTestInstallation, TokenSecret: token.Token,
		ActorID: 2, RepositoryID: repository, Families: []string{sdk.SnapshotFamilyIssue},
	})
	require.NoError(t, err)
	return decision, sdk.CreationRecoveryRequest{RepositoryID: fmt.Sprint(repository), ActorID: "2", Limit: 10}
}

func recoveryCreation(t *testing.T, repository, index int64) (*issues_model.Issue, *api.IssuePayload) {
	t.Helper()
	issue := insertSnapshotIssue(t, repository, index, "original title", "original body", 0)
	unittest.AssertSuccessfulInsert(t, &issues_model.ContentHistory{
		PosterID: 2, IssueID: issue.ID, EditedUnix: issue.CreatedUnix,
		ContentText: issue.Content, IsFirstCreated: true,
	})
	return issue, &api.IssuePayload{
		Action: api.HookIssueOpened, Index: index,
		Issue:      &api.Issue{ID: issue.ID, Index: index, Poster: &api.User{ID: 2}, Title: issue.Title, Body: issue.Content, Created: time.Unix(int64(issue.CreatedUnix), 0)},
		Repository: &api.Repository{ID: repository, Permissions: &api.Permission{Push: true}}, Sender: &api.User{ID: 2},
	}
}

func recoveryTask(t *testing.T, hookID int64, payload *api.IssuePayload) *webhook_model.HookTask {
	t.Helper()
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	task, err := webhook_model.CreateHookTask(t.Context(), &webhook_model.HookTask{
		HookID: hookID, EventType: webhook_module.HookEventIssues, PayloadVersion: 2, PayloadContent: string(encoded),
	})
	require.NoError(t, err)
	return task
}

func TestCreationRecoveryPagesRetainedTasksAndPreservesOriginalFacts(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	decision, req := recoveryAuthority(t, 1)
	issue, payload := recoveryCreation(t, 1, 171)
	first := recoveryTask(t, 1, payload)
	payload.Action = api.HookIssueEdited
	edited := recoveryTask(t, 1, payload)
	issue.Title, issue.Content, issue.ContentVersion = "changed title", "changed body", 1
	_, err := db.GetEngine(ctx).ID(issue.ID).Cols("title", "content", "content_version").Update(issue)
	require.NoError(t, err)
	req.Limit = 1
	page, err := NewService().ReadCreations(ctx, decision, req)
	require.NoError(t, err)
	require.False(t, page.Complete)
	require.True(t, page.ProvenanceComplete)
	require.True(t, page.RetentionLimited)
	require.Equal(t, fmt.Sprint(first.ID), page.OldestRetainedTaskID)
	require.Equal(t, fmt.Sprint(first.ID), page.NextCursor)
	require.Len(t, page.Creations, 1)
	got := page.Creations[0]
	require.True(t, got.Verified)
	require.True(t, got.CreatorWrite)
	require.Equal(t, first.UUID, got.DeliveryID)
	require.Equal(t, issues_model.ContentDigest("original title"), got.TitleDigest)
	require.Equal(t, issues_model.ContentDigest("original body"), got.ContentDigest)
	req.AfterID = page.NextCursor
	page, err = NewService().ReadCreations(ctx, decision, req)
	require.NoError(t, err)
	require.True(t, page.Complete)
	require.True(t, page.ProvenanceComplete)
	require.Empty(t, page.Creations)
	require.Equal(t, fmt.Sprint(edited.ID), page.NextCursor, "non-creation tasks still advance the scan")
	// Native replay gets a fresh delivery identity but retains creation identity.
	replayed, err := webhook_model.ReplayHookTask(ctx, 1, first.UUID)
	require.NoError(t, err)
	req.AfterID = page.NextCursor
	page, err = NewService().ReadCreations(ctx, decision, req)
	require.NoError(t, err)
	require.Len(t, page.Creations, 1)
	require.NotEqual(t, got.DeliveryID, page.Creations[0].DeliveryID)
	require.Equal(t, got.IssueID, page.Creations[0].IssueID)
	require.Equal(t, fmt.Sprint(replayed.ID), page.Creations[0].TaskID)
}

func TestCreationRecoveryIncompleteProvenanceNeverBecomesAbsence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*testing.T, *issues_model.Issue, *api.IssuePayload, *webhook_model.HookTask)
	}{
		{"unsupported payload", func(t *testing.T, _ *issues_model.Issue, _ *api.IssuePayload, task *webhook_model.HookTask) {
			task.PayloadVersion = 1
			_, err := db.GetEngine(t.Context()).ID(task.ID).Cols("payload_version").Update(task)
			require.NoError(t, err)
		}},
		{"deleted first history", func(t *testing.T, issue *issues_model.Issue, _ *api.IssuePayload, _ *webhook_model.HookTask) {
			_, err := db.GetEngine(t.Context()).Where("issue_id=?", issue.ID).Cols("is_deleted").Update(&issues_model.ContentHistory{IsDeleted: true})
			require.NoError(t, err)
		}},
		{"wrong first-history actor", func(t *testing.T, issue *issues_model.Issue, _ *api.IssuePayload, _ *webhook_model.HookTask) {
			_, err := db.GetEngine(t.Context()).Where("issue_id=?", issue.ID).Cols("poster_id").Update(&issues_model.ContentHistory{PosterID: 4})
			require.NoError(t, err)
		}},
		{"wrong first-history body", func(t *testing.T, issue *issues_model.Issue, _ *api.IssuePayload, _ *webhook_model.HookTask) {
			_, err := db.GetEngine(t.Context()).Where("issue_id=?", issue.ID).Cols("content_text").Update(&issues_model.ContentHistory{ContentText: "another body"})
			require.NoError(t, err)
		}},
		{"deleted issue", func(t *testing.T, issue *issues_model.Issue, _ *api.IssuePayload, _ *webhook_model.HookTask) {
			_, err := db.GetEngine(t.Context()).ID(issue.ID).Delete(new(issues_model.Issue))
			require.NoError(t, err)
		}},
		{"mismatched sender", func(t *testing.T, _ *issues_model.Issue, payload *api.IssuePayload, task *webhook_model.HookTask) {
			payload.Sender.ID = 4
			data, err := json.Marshal(payload)
			require.NoError(t, err)
			task.PayloadContent = string(data)
			_, err = db.GetEngine(t.Context()).ID(task.ID).Cols("payload_content").Update(task)
			require.NoError(t, err)
		}},
		{"wrong repository payload", func(t *testing.T, _ *issues_model.Issue, payload *api.IssuePayload, task *webhook_model.HookTask) {
			payload.Repository.ID = 2
			data, err := json.Marshal(payload)
			require.NoError(t, err)
			task.PayloadContent = string(data)
			_, err = db.GetEngine(t.Context()).ID(task.ID).Cols("payload_content").Update(task)
			require.NoError(t, err)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unittest.PrepareTestEnv(t)
			decision, req := recoveryAuthority(t, 1)
			issue, payload := recoveryCreation(t, 1, 172)
			task := recoveryTask(t, 1, payload)
			tc.alter(t, issue, payload, task)
			page, err := NewService().ReadCreations(t.Context(), decision, req)
			require.NoError(t, err)
			require.True(t, page.Complete)
			require.False(t, page.ProvenanceComplete)
			for _, item := range page.Creations {
				require.False(t, item.Verified)
			}
		})
	}
}

func TestCreationRecoveryRespectsRepositoryAndCredentialAuthority(t *testing.T) {
	unittest.PrepareTestEnv(t)
	_, payload := recoveryCreation(t, 2, 173)
	recoveryTask(t, 4, payload) // repo2 hook, outside the requested repo1
	decision, req := recoveryAuthority(t, 1)
	page, err := NewService().ReadCreations(t.Context(), decision, req)
	require.NoError(t, err)
	require.Empty(t, page.Creations)
	require.True(t, page.ProvenanceComplete)
	decision, req = recoveryAuthority(t, 2)
	decision.PublicOnly = true
	_, err = NewService().ReadCreations(t.Context(), decision, req)
	require.ErrorIs(t, err, ErrSnapshotNotFound)
	decision.PublicOnly = false
	decision.ResourceAllRepos = false
	decision.RepositoryIDs = []int64{1}
	_, err = NewService().ReadCreations(t.Context(), decision, req)
	require.ErrorIs(t, err, ErrSnapshotNotFound)
	decision.ResourceAllRepos = true
	decision.Scope = "read:repository"
	_, err = NewService().ReadCreations(t.Context(), decision, req)
	require.ErrorIs(t, err, ErrSnapshotInvalid)
}

func TestCreationRecoveryUnchangedBodyWithoutHistoryAndEditedMissingHistory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  int
		body     string
		verified bool
	}{
		{"unchanged native creation", 0, "original body", true},
		{"edited body without history", 1, "changed body", false},
		{"reverted body without history", 2, "original body", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unittest.PrepareTestEnv(t)
			ctx := t.Context()
			decision, req := recoveryAuthority(t, 1)
			issue, payload := recoveryCreation(t, 1, 174)
			_, err := db.GetEngine(ctx).Where("issue_id=?", issue.ID).Delete(new(issues_model.ContentHistory))
			require.NoError(t, err)
			issue.ContentVersion, issue.Content = tc.version, tc.body
			_, err = db.GetEngine(ctx).ID(issue.ID).Cols("content_version", "content").Update(issue)
			require.NoError(t, err)
			recoveryTask(t, 1, payload)
			page, err := NewService().ReadCreations(ctx, decision, req)
			require.NoError(t, err)
			require.Len(t, page.Creations, 1)
			require.Equal(t, tc.verified, page.Creations[0].Verified)
			require.Equal(t, tc.verified, page.ProvenanceComplete)
		})
	}
}

func TestCreationRecoveryOnPostgres(t *testing.T) {
	useEnrolledAuthorityPostgres(t)
	t.Run("paging and replay", TestCreationRecoveryPagesRetainedTasksAndPreservesOriginalFacts)
	t.Run("incomplete provenance", TestCreationRecoveryIncompleteProvenanceNeverBecomesAbsence)
	t.Run("credential authority", TestCreationRecoveryRespectsRepositoryAndCredentialAuthority)
	t.Run("normal creation and edit revert", TestCreationRecoveryUnchangedBodyWithoutHistoryAndEditedMissingHistory)
}
