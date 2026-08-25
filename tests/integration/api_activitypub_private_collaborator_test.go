// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	perm_model "forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/git"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/federation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActivityPubFederatedPrivateCollaborator verifies the federated private
// repository access story end to end:
//
//  1. a remote user follows a local private repository (materialising their
//     federated user record locally);
//  2. the owner adds the remote user as a read-only collaborator via the API;
//  3. the remote user's signed requests can now fetch the private repository
//     actor (authorized fetch);
//  4. another remote user, who is not a collaborator, still gets 404 and
//     cannot learn the repository exists.
//
// This is the "only that user" guarantee: access is granted per federated
// actor, not per instance.
func TestActivityPubFederatedPrivateCollaborator(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	federation.Init()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := int64(2) // private repository in the fixtures
		localRepoActor := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID)).String()

		// 1. Remote person 15 follows the local owner's profile inbox. This
		// materialises the remote user locally without touching the private
		// repository (whose inbox rejects unknown actors).
		localUserInbox := u.JoinPath("/api/v1/activitypub/user-id/2/inbox").String()

		ctx, _ := contexttest.MockAPIContext(t, localUserInbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c15, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey, mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		followActivity := fmt.Appendf(nil,
			`{"type":"Follow","actor":"%s/api/v1/activitypub/user-id/15","object":"%s"}`,
			federatedSrv.URL, u.JoinPath("/api/v1/activitypub/user-id/2").String())
		resp, err := c15.Post(followActivity, localUserInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)

		// Before being granted access, the remote user cannot fetch the
		// private repository actor: 404.
		resp, err = c15.Get(localRepoActor)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "remote user must not see the private repo before being granted access")

		// 2. The owner adds the remote user as a read-only collaborator.
		owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		session := loginUser(t, owner.LoginName)
		token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeAll)

		distantFederatedUser := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "15"})
		collaborator := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: distantFederatedUser.UserID})
		// The federated user cannot log in but is addable as a collaborator.
		require.False(t, collaborator.IsActive)
		require.True(t, collaborator.IsActivityPub())

		collabName := url.PathEscape(collaborator.Name)
		addReq := NewRequestWithJSON(t, "PUT",
			fmt.Sprintf("/api/v1/repos/%s/%s/collaborators/%s", owner.Name, "repo2", collabName),
			map[string]any{}).AddTokenAuth(token)
		session.MakeRequest(t, addReq, http.StatusNoContent)

		// 3. The remote user's signed request now reaches the private
		// repository actor.
		resp, err = c15.Get(localRepoActor)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode, "the federated collaborator must be able to fetch the private repository actor")

		// 4. Another remote user (person 30, same mock instance) is NOT a
		// collaborator and must still get 404: access is per actor, not per
		// instance.
		c30, err := cf.WithKeysDirect(ctx, mock.Persons[1].PrivKey, mock.Persons[1].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)
		resp, err = c30.Get(localRepoActor)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "a non-collaborator on the same instance must not see the private repo")

		// Sanity: the collaborator's permission is read-only.
		perm, err := access_model.GetUserRepoPermission(ctx,
			unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2}), collaborator)
		require.NoError(t, err)
		assert.Equal(t, perm_model.AccessModeRead, perm.AccessMode)

		// 5. The owner issues a read-only git token for the federated
		// collaborator, which the remote user uses to clone the private
		// repository over git smart HTTP.
		tokenReq := NewRequest(t, "POST",
			fmt.Sprintf("/api/v1/repos/%s/%s/collaborators/%s/token", owner.Name, "repo2", collabName)).AddTokenAuth(token)
		tokenResp := session.MakeRequest(t, tokenReq, http.StatusCreated)

		var tokenOut struct {
			Name  string `json:"name"`
			Token string `json:"token"`
		}
		require.NoError(t, json.Unmarshal(tokenResp.Body.Bytes(), &tokenOut))
		require.NotEmpty(t, tokenOut.Token)

		// Clone as the federated user using the token (oauth2:<token> as the
		// credential, matching the git smart HTTP token convention).
		cloneURL := fmt.Sprintf("%s%s/%s.git", u, owner.Name, "repo2")
		parsed, err := url.Parse(cloneURL)
		require.NoError(t, err)
		parsed.User = url.UserPassword("oauth2", tokenOut.Token)

		cloneDir := t.TempDir()
		require.NoError(t, git.CloneWithArgs(t.Context(), git.AllowLFSFiltersArgs(), parsed.String(), cloneDir, git.CloneRepoOptions{}))
		require.FileExists(t, filepath.Join(cloneDir, "Home.md"))
	})
}
