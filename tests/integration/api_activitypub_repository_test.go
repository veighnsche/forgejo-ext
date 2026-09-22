// Copyright 2024, 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"forgejo.org/modules/activitypub"
	forgefed_modules "forgejo.org/modules/forgefed"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivityPubRepository(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := 2
		localRepository := fmt.Sprintf("api/v1/activitypub/repository-id/%d", repositoryID)
		localRepositoryURL := fmt.Sprintf("%s%s", u, localRepository)

		ctx, _ := contexttest.MockAPIContext(t, localRepositoryURL)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		resp, err := c.GetBody(localRepositoryURL)
		require.NoError(t, err)
		assert.Contains(t, string(resp), "@context")

		var repository forgefed_modules.Repository
		err = repository.UnmarshalJSON(resp)
		require.NoError(t, err)

		assert.Regexp(t, localRepository, repository.GetID().String())
	})
}

func TestActivityPubMissingRepository(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.SignatureEnforced, false)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	repositoryID := 9999999
	req := NewRequest(t, "GET", fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID))
	resp := MakeRequest(t, req, http.StatusNotFound)
	assert.Contains(t, resp.Body.String(), "repository does not exist")
}

func TestActivityPubRepositoryInboxValid(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.SignatureEnforced, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := 2
		timeNow := time.Now().UTC()
		localRepo2 := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID)).String()
		localRepoInbox := fmt.Sprintf("%s/inbox", localRepo2)

		ctx, _ := contexttest.MockAPIContext(t, localRepoInbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		activity1, err := json.Marshal(map[string]any{
			"type":      "Like",
			"startTime": timeNow.Format(time.RFC3339),
			"actor":     federatedSrv.URL + "/api/v1/activitypub/user-id/15",
			"object":    localRepo2,
		})
		if err != nil {
			require.Errorf(t, err, "failed to marshal: activityUser15LikesRepo2")
		}
		t.Logf("activity: %s", activity1)
		resp, err := c.Post(activity1, localRepoInbox)

		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	})
}

func TestActivityPubRepositoryInboxInvalid(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := 2
		localRepo2Inbox := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d/inbox", repositoryID)).String()

		ctx, _ := contexttest.MockAPIContext(t, localRepo2Inbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		activity := []byte(`{"type":"Wrong"}`)
		resp, err := c.Post(activity, localRepo2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode)
	})
}
