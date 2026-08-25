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

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/federation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActivityPubRepositoryInboxFollow verifies the inbound repository-follow
// handshake: a remote user sends Follow targeting a local repository actor,
// the follower is recorded in federated_repo_follower and an Accept activity
// is delivered back to the remote user's inbox.
func TestActivityPubRepositoryInboxFollow(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	federation.Init()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := int64(1) // public repository: federation must not expose private ones
		localRepoInbox := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d/inbox", repositoryID)).String()
		localRepoActor := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID)).String()

		ctx, _ := contexttest.MockAPIContext(t, localRepoInbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		// 1. Remote user 15 follows the local repository.
		followActivity := fmt.Appendf(nil,
			`{"type":"Follow",`+
				`"actor":"%s/api/v1/activitypub/user-id/15",`+
				`"object":"%s"}`,
			federatedSrv.URL, localRepoActor)
		resp, err := c.Post(followActivity, localRepoInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// The remote follower is materialised and recorded as a repo follower.
		distantFederatedUser := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "15"})
		follower := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: distantFederatedUser.UserID})
		ok, err := repo_model.IsFollowingRepo(ctx, follower.ID, repositoryID)
		require.NoError(t, err)
		require.True(t, ok, "remote user should be recorded as following the repository")

		// 2. Re-following is idempotent (still 204).
		resp, err = c.Post(followActivity, localRepoInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// 3. The remote server receives an Accept for the follow handshake.
		require.Eventually(t, func() bool {
			return containsActivityType(mock.LastPost, "Accept")
		}, 5*time.Second, 100*time.Millisecond, "distant server did not receive an Accept")

		// 4. The repository actor advertises its followers collection and the
		// collection lists the remote follower.
		localRepoFollowers := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d/followers", repositoryID)).String()
		body, err := c.GetBody(localRepoFollowers)
		require.NoError(t, err)
		assert.Contains(t, string(body), fmt.Sprintf("%s/api/v1/activitypub/user-id/15", federatedSrv.URL),
			"followers collection must list the remote follower")
	})
}

func containsActivityType(lastPost, activityType string) bool {
	return strings.Contains(lastPost, fmt.Sprintf(`"type":"%s"`, activityType))
}
