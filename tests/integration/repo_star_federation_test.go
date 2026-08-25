// Copyright 2024 The Forgejo Authors c/o Codeberg e.V.. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/forgefed"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/validation"
	"forgejo.org/services/federation"
	"forgejo.org/tests"

	ap "github.com/go-ap/activitypub"
	"github.com/stretchr/testify/require"
)

func TestActivityPubRepoFollowing(t *testing.T) {
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

	t.Run("Add a following repo", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		link := fmt.Sprintf("/%s/settings", repo.FullName())

		req := NewRequestWithValues(t, "POST", link, map[string]string{
			"action":          "federation",
			"following_repos": fmt.Sprintf("%s/api/v1/activitypub/repository-id/1", federatedSrv.URL),
		})
		session.MakeRequest(t, req, http.StatusSeeOther)

		// Verify it was added.
		federationHost := unittest.AssertExistsAndLoadBean(t, &forgefed.FederationHost{HostFqdn: "127.0.0.1"})
		unittest.AssertExistsAndLoadBean(t, &repo_model.FollowingRepo{
			ExternalID:       "1",
			FederationHostID: federationHost.ID,
		})
	})

	t.Run("Star a repo having a following repo", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		repoLink := fmt.Sprintf("/%s", repo.FullName())
		link := fmt.Sprintf("%s/action/star", repoLink)
		req := NewRequest(t, "POST", link)

		session.MakeRequest(t, req, http.StatusOK)

		// Delivery is asynchronous via the delivery queue: wait for the
		// distant server to receive the Like activity.
		require.Eventually(t, func() bool {
			like := fm.ForgeLike{}
			if err := like.UnmarshalJSON([]byte(mock.LastPost)); err != nil {
				return false
			}
			if isValid, _ := validation.IsValid(like); !isValid {
				return false
			}
			return like.Type == ap.LikeType &&
				strings.HasSuffix(like.Object.GetLink().String(), "/api/v1/activitypub/repository-id/1")
		}, 5*time.Second, 100*time.Millisecond, "distant server did not receive a Like activity")
	})
}
