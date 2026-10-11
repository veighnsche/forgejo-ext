// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"testing"

	auth_model "forgejo.org/models/auth"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	webhook_model "forgejo.org/models/webhook"
	api "forgejo.org/modules/structs"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

func TestAPICreateHook(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 37})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID})

	// user1 is an admin user
	session := loginUser(t, "user1")
	token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository)
	req := NewRequestWithJSON(t, "POST", fmt.Sprintf("/api/v1/repos/%s/%s/%s", owner.Name, repo.Name, "hooks"), api.CreateHookOption{
		Type: "gitea",
		Config: api.CreateHookOptionConfig{
			"content_type": "json",
			"url":          "http://example.com/",
		},
		AuthorizationHeader: "Bearer s3cr3t",
	}).AddTokenAuth(token)
	resp := MakeRequest(t, req, http.StatusCreated)

	var apiHook *api.Hook
	DecodeJSON(t, resp, &apiHook)
	assert.Equal(t, "http://example.com/", apiHook.Config["url"])
	assert.Equal(t, "http://example.com/", apiHook.URL)
	assert.Equal(t, "Bearer s3cr3t", apiHook.AuthorizationHeader)

	patch := NewRequestWithJSON(t, "PATCH", fmt.Sprintf("/api/v1/repos/%s/%s/hooks/%d", owner.Name, repo.Name, apiHook.ID), api.EditHookOption{
		Config: map[string]string{"secret": "rotated-hook-secret"},
	}).AddTokenAuth(token)
	patchResp := MakeRequest(t, patch, http.StatusOK)
	assert.NotContains(t, patchResp.Body.String(), "rotated-hook-secret")
	stored, err := webhook_model.GetWebhookByID(t.Context(), apiHook.ID)
	assert.NoError(t, err)
	assert.Equal(t, "rotated-hook-secret", stored.Secret)

	patchWithoutSecret := NewRequestWithJSON(t, "PATCH", fmt.Sprintf("/api/v1/repos/%s/%s/hooks/%d", owner.Name, repo.Name, apiHook.ID), api.EditHookOption{
		Config: map[string]string{"content_type": "json"},
		Events: []string{"issues"},
	}).AddTokenAuth(token)
	MakeRequest(t, patchWithoutSecret, http.StatusOK)
	stored, err = webhook_model.GetWebhookByID(t.Context(), apiHook.ID)
	assert.NoError(t, err)
	assert.Equal(t, "rotated-hook-secret", stored.Secret)
}
