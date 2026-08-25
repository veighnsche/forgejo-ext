// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/forgefed"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/validation"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/federation"
	"forgejo.org/tests"

	ap "github.com/go-ap/activitypub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActivityPubRepositoryInboxUndoLike verifies the federated unstar flow:
// a remote user first sends a Like (star) to a local repository inbox and the
// repository becomes starred; then the same remote user sends an Undo(Like)
// activity and the repository is unstarred again.
func TestActivityPubRepositoryInboxUndoLike(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := int64(1) // public repository: federation must not expose private ones
		timeNow := time.Now().UTC()
		localRepoInbox := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d/inbox", repositoryID)).String()
		repoActor := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID)).String()

		ctx, _ := contexttest.MockAPIContext(t, localRepoInbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		// 1. Remote user stars the repository.
		likeActivity := fmt.Appendf(nil,
			`{"type":"Like",`+
				`"startTime":"%s",`+
				`"actor":"%s/api/v1/activitypub/user-id/15",`+
				`"object":"%s"}`,
			timeNow.Format(time.RFC3339),
			federatedSrv.URL, repoActor)
		resp, err := c.Post(likeActivity, localRepoInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// The federation host and remote user are now known locally.
		federationHost := unittest.AssertExistsAndLoadBean(t, &forgefed.FederationHost{HostFqdn: "127.0.0.1"})

		federatedUser := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "15", FederationHostID: federationHost.ID})
		require.True(t, repo_model.IsStaring(ctx, federatedUser.UserID, repositoryID),
			"repository should be starred after receiving a Like")

		// 2. Remote user unstars the repository with Undo(Like).
		undoLikeActivity := fmt.Appendf(nil,
			`{"type":"Undo",`+
				`"startTime":"%s",`+
				`"actor":"%s/api/v1/activitypub/user-id/15",`+
				`"object":{"type":"Like","actor":"%s/api/v1/activitypub/user-id/15","object":"%s"}}`,
			timeNow.Add(time.Second).Format(time.RFC3339),
			federatedSrv.URL, federatedSrv.URL, repoActor)
		resp, err = c.Post(undoLikeActivity, localRepoInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// Confirm the repository is no longer starred.
		require.False(t, repo_model.IsStaring(ctx, federatedUser.UserID, repositoryID),
			"repository should be unstarred after receiving an Undo(Like)")

		// 3. Replaying the same Undo(Like) must be rejected (out-of-order guard).
		resp, err = c.Post(undoLikeActivity, localRepoInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode)
	})
}

// TestActivityPubRepoUnstarOutbound verifies that when a local user unstars a
// repository that has a federated following repo, an Undo(Like) activity is
// delivered to the distant forge peer (so its star count stays consistent).
func TestActivityPubRepoUnstarOutbound(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()

	federation.Init()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user.ID})
	session := loginUser(t, user.Name)

	// Add a following repo pointing at the distant server.
	link := fmt.Sprintf("/%s/settings", repo.FullName())
	req := NewRequestWithValues(t, "POST", link, map[string]string{
		"action":          "federation",
		"following_repos": fmt.Sprintf("%s/api/v1/activitypub/repository-id/1", federatedSrv.URL),
	})
	session.MakeRequest(t, req, http.StatusSeeOther)

	// Star the repo -> the distant server receives a Like.
	repoLink := fmt.Sprintf("/%s", repo.FullName())
	req = NewRequest(t, "POST", repoLink+"/action/star")
	session.MakeRequest(t, req, http.StatusOK)

	// Unstar the repo -> the distant server must receive an Undo(Like).
	req = NewRequest(t, "POST", repoLink+"/action/unstar")
	session.MakeRequest(t, req, http.StatusOK)

	// Delivery is asynchronous via the delivery queue: wait for the distant
	// server to receive the Undo(Like) activity.
	require.Eventually(t, func() bool {
		undo := fm.ForgeUndoLike{}
		if err := undo.UnmarshalJSON([]byte(mock.LastPost)); err != nil {
			return false
		}
		if isValid, _ := validation.IsValid(undo); !isValid {
			return false
		}
		if undo.Type != ap.UndoType {
			return false
		}
		inner, ok := undo.Object.(*ap.Activity)
		if !ok || inner.Type != ap.LikeType {
			return false
		}
		return strings.HasSuffix(inner.Object.GetLink().String(), "/api/v1/activitypub/repository-id/1")
	}, 5*time.Second, 100*time.Millisecond, "distant server did not receive an Undo(Like) activity")
}
