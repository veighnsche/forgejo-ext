// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	webhook_model "forgejo.org/models/webhook"
	"forgejo.org/modules/queue"
	webhook_module "forgejo.org/modules/webhook"
	issue_service "forgejo.org/services/issue"
	operation_service "forgejo.org/services/nativeoperation"
	webhook_service "forgejo.org/services/webhook"

	"github.com/stretchr/testify/require"
)

func TestNativeIssueCreationRecoversAfterFailedHTTPDelivery(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	hook := &webhook_model.Webhook{
		RepoID: 1, URL: server.URL, HTTPMethod: "POST", IsActive: true,
		Type: webhook_module.FORGEJO, ContentType: webhook_model.ContentTypeJSON, Events: `{"send_everything":true}`,
	}
	require.NoError(t, webhook_model.CreateWebhook(ctx, hook, ""))
	repo, err := repo_model.GetRepositoryByID(ctx, 1)
	require.NoError(t, err)
	actor, err := user_model.GetUserByID(ctx, 2)
	require.NoError(t, err)
	issue := &issues_model.Issue{RepoID: repo.ID, PosterID: actor.ID, Poster: actor, Title: "recover missed creation", Content: "original body"}
	// The normal service owns creation and the original hook
	// producer. The existing serving queue performs the real failed HTTP send.
	require.NoError(t, issue_service.NewIssue(ctx, repo, issue, nil, nil, nil))
	var task webhook_model.HookTask
	has, err := db.GetEngine(ctx).Where("hook_id=? AND event_type=?", hook.ID, webhook_module.HookEventIssues).Get(&task)
	require.NoError(t, err)
	require.True(t, has, "the normal native creation producer must retain its task before delivery")
	require.NoError(t, queue.GetManager().FlushAll(ctx, 10*time.Second))
	require.Eventually(t, func() bool {
		loaded, err := webhook_model.GetHookTaskByID(ctx, task.ID)
		if err != nil || !loaded.IsDelivered || loaded.ResponseInfo == nil || loaded.ResponseInfo.Status != http.StatusServiceUnavailable {
			return false
		}
		task = *loaded
		return true
	}, 10*time.Second, 10*time.Millisecond)
	require.False(t, task.IsSucceed)
	require.Equal(t, int64(1), attempts.Load())
	// IsDelivered denotes one attempted send; calling Deliver again does not
	// retry the failed task. Recovery must read the retained original record.
	require.NoError(t, webhook_service.Deliver(ctx, &task))
	require.Equal(t, int64(1), attempts.Load())
	token := &auth_model.AccessToken{UID: actor.ID, Name: "creation-recovery-reader", Scope: "read:issue", ResourceAllRepos: true}
	require.NoError(t, auth_model.NewAccessToken(ctx, token))
	const installation = "11111111-2222-4333-8444-555555555555"
	_, err = extensionauth.EnrollBinding(ctx, installation, token.ID, actor.ID, repo.ID, extensionauth.KindMerge)
	require.NoError(t, err)
	decision, err := extensionauth.VerifySnapshotRead(ctx, extensionauth.SnapshotReadRequest{
		InstallationID: installation, TokenSecret: token.Token, ActorID: actor.ID, RepositoryID: repo.ID, Families: []string{sdk.SnapshotFamilyIssue},
	})
	require.NoError(t, err)
	page, err := operation_service.NewService().ReadCreations(ctx, decision, sdk.CreationRecoveryRequest{RepositoryID: fmt.Sprint(repo.ID), ActorID: fmt.Sprint(actor.ID), Limit: 10})
	require.NoError(t, err)
	require.True(t, page.ProvenanceComplete)
	require.True(t, page.Complete)
	require.True(t, page.RetentionLimited)
	require.Len(t, page.Creations, 1)
	require.True(t, page.Creations[0].Verified)
	require.True(t, page.Creations[0].CreatorWrite)
	require.Equal(t, fmt.Sprint(issue.ID), page.Creations[0].IssueID)
	require.Equal(t, task.UUID, page.Creations[0].DeliveryID)
}
