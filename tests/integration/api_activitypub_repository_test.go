// Copyright 2024, 2025 The Forgejo Authors. All rights reserved.
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
	"forgejo.org/models/unittest"
	"forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	forgefed_modules "forgejo.org/modules/forgefed"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"

	ap "github.com/go-ap/activitypub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivityPubRepository(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := 1 // public repository: federation must not expose private ones

		localRepository := fmt.Sprintf("%sapi/v1/activitypub/repository-id/%d", u, repositoryID)

		ctx, _ := contexttest.MockAPIContext(t, localRepository)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		resp, err := c.GetBody(localRepository)
		require.NoError(t, err)
		assert.Contains(t, string(resp), "@context")

		var repository forgefed_modules.Repository
		err = repository.UnmarshalJSON(resp)
		require.NoError(t, err)

		assert.Regexp(t, fmt.Sprintf("activitypub/repository-id/%d$", repositoryID), repository.GetID().String())

		// The repository actor must expose a public key so peers can verify
		// signatures made on behalf of the repository.
		assert.NotEmpty(t, repository.PublicKey.PublicKeyPem, "repository actor must expose a publicKey")
		assert.True(t, strings.HasSuffix(repository.PublicKey.ID.String(),
			fmt.Sprintf("/api/v1/activitypub/repository-id/%d#main-key", repositoryID)),
			"publicKey id must reference the repository actor's main key")

		// ForgeFed vocabulary: the team must include the repository owner and
		// non-fork repositories must not expose a forkedFrom.
		// The team may unmarshal as an ItemCollection or, for a single member,
		// as a bare IRI depending on the go-ap JSON handling.
		teamHasMember := false
		if team, ok := repository.Team.(ap.ItemCollection); ok {
			teamHasMember = len(team) > 0
		} else if link := repository.Team.GetLink(); link != "" {
			teamHasMember = true
		}
		require.True(t, teamHasMember, "team must contain at least the owner")
		assert.Nil(t, repository.ForkedFrom, "non-fork repository must not expose forkedFrom")
	})
}

func TestActivityPubMissingRepository(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	repositoryID := 9999999
	// Signature verification runs before the repository lookup: unsigned
	// requests are rejected without revealing whether the repository exists.
	req := NewRequest(t, "GET", fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID))
	resp := MakeRequest(t, req, http.StatusBadRequest)
	assert.Contains(t, resp.Body.String(), "request signature verification failed")
}

func TestActivityPubRepositoryInboxValid(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := 1 // public repository: federation must not expose private ones
		timeNow := time.Now().UTC()
		localRepoInbox := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d/inbox", repositoryID)).String()

		ctx, _ := contexttest.MockAPIContext(t, localRepoInbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		activity1 := fmt.Appendf(nil,
			`{"type":"Like",`+
				`"startTime":"%s",`+
				`"actor":"%s/api/v1/activitypub/user-id/15",`+
				`"object":"%s"}`,
			timeNow.Format(time.RFC3339),
			federatedSrv.URL, u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID)).String())
		t.Logf("activity: %s", activity1)
		resp, err := c.Post(activity1, localRepoInbox)

		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		federationHost := unittest.AssertExistsAndLoadBean(t, &forgefed.FederationHost{HostFqdn: "127.0.0.1"})
		federatedUser := unittest.AssertExistsAndLoadBean(t, &user.FederatedUser{ExternalID: "15", FederationHostID: federationHost.ID})
		unittest.AssertExistsAndLoadBean(t, &user.User{ID: federatedUser.UserID})

		// A like activity by a different user of the same federated host.
		activity2 := fmt.Appendf(nil,
			`{"type":"Like",`+
				`"startTime":"%s",`+
				`"actor":"%s/api/v1/activitypub/user-id/30",`+
				`"object":"%s"}`,
			// Make sure this activity happens later then the one before
			timeNow.Add(time.Second).Format(time.RFC3339),
			federatedSrv.URL, u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", repositoryID)).String())
		t.Logf("activity: %s", activity2)
		resp, err = c.Post(activity2, localRepoInbox)

		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		federatedUser = unittest.AssertExistsAndLoadBean(t, &user.FederatedUser{ExternalID: "30", FederationHostID: federationHost.ID})
		unittest.AssertExistsAndLoadBean(t, &user.User{ID: federatedUser.UserID})

		// The same user sends another like activity
		otherRepositoryID := 4 // public repository
		otherRepoInboxURL := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d/inbox", otherRepositoryID)).String()
		activity3 := fmt.Appendf(nil,
			`{"type":"Like",`+
				`"startTime":"%s",`+
				`"actor":"%s/api/v1/activitypub/user-id/30",`+
				`"object":"%s"}`,
			// Make sure this activity happens later then the ones before
			timeNow.Add(time.Second*2).Format(time.RFC3339),
			federatedSrv.URL, u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d", otherRepositoryID)).String())
		t.Logf("activity: %s", activity3)
		resp, err = c.Post(activity3, otherRepoInboxURL)

		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		federatedUser = unittest.AssertExistsAndLoadBean(t, &user.FederatedUser{ExternalID: "30", FederationHostID: federationHost.ID})
		unittest.AssertExistsAndLoadBean(t, &user.User{ID: federatedUser.UserID})

		// Replay activity2.
		resp, err = c.Post(activity2, localRepoInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode)
	})
}

func TestActivityPubRepositoryInboxInvalid(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		apServerActor := user.NewAPServerActor()
		repositoryID := 1 // public repository
		localRepoInbox := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d/inbox", repositoryID)).String()

		ctx, _ := contexttest.MockAPIContext(t, localRepoInbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeys(ctx, apServerActor, apServerActor.KeyID(), nil)
		require.NoError(t, err)

		// The activity carries the instance actor so it passes the actor
		// binding check, and fails processing as an unsupported type.
		ghostActorURI := strings.TrimSuffix(setting.AppURL, "/") + "/api/v1/activitypub/actor"
		activity := fmt.Appendf(nil, `{"type":"Wrong","actor":"%s"}`, ghostActorURI)

		resp, err := c.Post(activity, localRepoInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode)
	})
}

// TestActivityPubPrivateRepositoryHidden verifies that the repository actor
// of a private repository is not served to federation peers without access:
// the endpoint answers 404 so it does not leak the repository's existence,
// clone URLs or collaborators.
func TestActivityPubPrivateRepositoryHidden(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := 2 // private repository in the fixtures

		localRepository := fmt.Sprintf("%sapi/v1/activitypub/repository-id/%d", u, repositoryID)
		ctx, _ := contexttest.MockAPIContext(t, localRepository)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		// Validly signed request from a federation peer without access: the
		// repository must not be revealed.
		resp, err := c.Get(localRepository)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

// TestActivityPubRepositoryInboxImpersonation verifies that a validly
// signed request whose payload claims an actor hosted on a different host
// than the signing key is rejected: the signature must bind to the actor
// the activity claims.
func TestActivityPubRepositoryInboxImpersonation(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repositoryID := 1 // public repository: impersonation is rejected
		// regardless of the repository's visibility
		localRepoInbox := u.JoinPath(fmt.Sprintf("/api/v1/activitypub/repository-id/%d/inbox", repositoryID)).String()

		ctx, _ := contexttest.MockAPIContext(t, localRepoInbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		// Signed by the mock server's person 15, but claiming an actor on
		// another host.
		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		activity := fmt.Appendf(nil,
			`{"type":"Like","actor":"http://127.0.0.1:1/api/v1/activitypub/user-id/15","object":"%s/api/v1/activitypub/repository-id/%d"}`,
			u, repositoryID)
		resp, err := c.Post(activity, localRepoInbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}
