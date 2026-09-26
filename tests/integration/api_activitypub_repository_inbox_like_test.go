// Copyright 2024, 2025, 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"forgejo.org/models/forgefed"
	"forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivityPubRepositoryInboxLike(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := int64(2)
		timeNow := time.Now().UTC()
		localRepo2 := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID)).String()
		localRepo2Inbox := fmt.Sprintf("%s/inbox", localRepo2)

		ctx, _ := contexttest.MockAPIContext(t, localRepo2Inbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey, mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		distantActorUser15 := fmt.Sprintf("%s/api/v1/activitypub/user-id/15", federatedSrv.URL)

		repo2 := unittest.AssertExistsAndLoadBean(t, &repo.Repository{ID: repositoryID})
		assert.Equal(t, 1, repo2.NumStars)

		var federationHost *forgefed.FederationHost

		t.Run("Like repo", func(t *testing.T) {
			activityUser15LikesRepo2, err := json.Marshal(map[string]any{
				"type":      "Like",
				"startTime": timeNow.Format(time.RFC3339),
				"actor":     distantActorUser15,
				"object":    localRepo2,
			})
			require.NoError(t, err, "failed to marshal: activityUser15LikesRepo2")

			resp, err := c.Post(activityUser15LikesRepo2, localRepo2Inbox)
			require.NoError(t, err)
			assert.Equal(t, http.StatusNoContent, resp.StatusCode)

			federationHost = unittest.AssertExistsAndLoadBean(t, &forgefed.FederationHost{HostFqdn: "127.0.0.1"})
			federatedUser := unittest.AssertExistsAndLoadBean(t, &user.FederatedUser{ExternalID: "15", FederationHostID: federationHost.ID})
			unittest.AssertExistsAndLoadBean(t, &user.User{ID: federatedUser.UserID})
			repo2 = unittest.AssertExistsAndLoadBean(t, &repo.Repository{ID: repositoryID})
			assert.Equal(t, 2, repo2.NumStars)
		})

		var (
			distantActorUser30       string
			activityUser30LikesRepo2 []byte
		)

		t.Run("Like repo from a different user of the same federated host", func(t *testing.T) {
			distantActorUser30 = fmt.Sprintf("%s/api/v1/activitypub/user-id/30", federatedSrv.URL)
			activityUser30LikesRepo2, err = json.Marshal(map[string]any{
				"type":      "Like",
				"startTime": timeNow.Add(time.Second).Format(time.RFC3339),
				"actor":     distantActorUser30,
				"object":    localRepo2,
			})
			require.NoError(t, err, "failed to marshal: activityUser30LikesRepo2")

			resp, err := c.Post(activityUser30LikesRepo2, localRepo2Inbox)
			require.NoError(t, err)
			assert.Equal(t, http.StatusNoContent, resp.StatusCode)

			federatedUser := unittest.AssertExistsAndLoadBean(t, &user.FederatedUser{ExternalID: "30", FederationHostID: federationHost.ID})
			unittest.AssertExistsAndLoadBean(t, &user.User{ID: federatedUser.UserID})
			repo2 = unittest.AssertExistsAndLoadBean(t, &repo.Repository{ID: repositoryID})
			assert.Equal(t, 3, repo2.NumStars)
		})

		t.Run("Resend a second activity", func(t *testing.T) {
			secondActivityUser30LikesRepo2, err := json.Marshal(map[string]any{
				"type":      "Like",
				"startTime": timeNow.Add(time.Second).Format(time.RFC3339),
				"actor":     distantActorUser30,
				"object":    localRepo2,
			})
			require.NoError(t, err, "failed to marshal: secondActivityUser30LikesRepo2")

			resp, err := c.Post(secondActivityUser30LikesRepo2, localRepo2Inbox)
			require.NoError(t, err)
			assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode)

			repo2 = unittest.AssertExistsAndLoadBean(t, &repo.Repository{ID: repositoryID})
			assert.Equal(t, 3, repo2.NumStars)
		})

		t.Run("Replay like", func(t *testing.T) {
			resp, err := c.Post(activityUser30LikesRepo2, localRepo2Inbox)
			require.NoError(t, err)
			assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode)
		})
	})
}
