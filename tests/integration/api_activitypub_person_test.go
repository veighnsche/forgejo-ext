// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/federation"
	"forgejo.org/tests"

	ap "github.com/go-ap/activitypub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivityPubPerson(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, localUrl *url.URL) {
		defer test.MockVariableValue(&setting.AppURL, localUrl.String())()

		localUserID := 2
		localUserName := "user2"
		localUserURL := fmt.Sprintf("%sapi/v1/activitypub/user-id/%d", localUrl, localUserID)

		// Unsigned request
		t.Run("UnsignedRequest", func(t *testing.T) {
			req := NewRequest(t, "GET", localUserURL)
			MakeRequest(t, req, http.StatusBadRequest)
		})

		// Signed request
		t.Run("SignedRequestValidation", func(t *testing.T) {
			ctx, _ := contexttest.MockAPIContext(t, localUserURL)
			cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
			require.NoError(t, err)

			c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
				mock.Persons[0].KeyID(federatedSrv.URL), nil)
			require.NoError(t, err)

			resp, err := c.GetBody(localUserURL)
			require.NoError(t, err)

			var person ap.Person
			err = person.UnmarshalJSON(resp)
			require.NoError(t, err)

			assert.Equal(t, ap.PersonType, person.Type)
			assert.Equal(t, localUserName, person.PreferredUsername.String())
			assert.Regexp(t, fmt.Sprintf("activitypub/user-id/%d$", localUserID), person.GetID())
			assert.Regexp(t, fmt.Sprintf("activitypub/user-id/%d/inbox$", localUserID), person.Inbox.GetID().String())
			assert.Regexp(t, fmt.Sprintf("activitypub/user-id/%d/outbox$", localUserID), person.Outbox.GetID().String())

			assert.NotNil(t, person.PublicKey)
			assert.Regexp(t, fmt.Sprintf("activitypub/user-id/%d#main-key$", localUserID), person.PublicKey.ID)

			assert.NotNil(t, person.PublicKey.PublicKeyPem)
			assert.Regexp(t, "^-----BEGIN PUBLIC KEY-----", person.PublicKey.PublicKeyPem)
		})
	})
}

func TestActivityPubMissingPerson(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.SignatureEnforced, false)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	req := NewRequest(t, "GET", "/api/v1/activitypub/user-id/999999999")
	resp := MakeRequest(t, req, http.StatusNotFound)
	assert.Contains(t, resp.Body.String(), "user does not exist")
}

func TestActivityPubPersonInbox(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		defer test.MockVariableValue(&setting.AppURL, u.String())()
		user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

		user1url := u.JoinPath("/api/v1/activitypub/user-id/1").String() + "#main-key"
		user2inboxurl := u.JoinPath("/api/v1/activitypub/user-id/2/inbox").String()
		ctx, _ := contexttest.MockAPIContext(t, user2inboxurl)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)
		c, err := cf.WithKeys(ctx, user1, user1url, nil)
		require.NoError(t, err)

		// invalid request is rejected
		resp, err := c.Post([]byte{}, user2inboxurl)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode)
	})
}

func TestActivityPubPersonOutbox(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		defer test.MockVariableValue(&setting.AppURL, u.String())()
		user2outboxurl := u.JoinPath("/api/v1/activitypub/user-id/2/outbox").String()

		ctx, _ := contexttest.MockAPIContext(t, user2outboxurl)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		// request outbox
		resp, err := c.Get(user2outboxurl)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestActivityPubPersonVerifyKeyID(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.SignatureEnforced, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	federation.Init()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, localUrl *url.URL) {
		defer test.MockVariableValue(&setting.AppURL, localUrl.String())()

		distantURL := federatedSrv.URL
		distantUser15URL := fmt.Sprintf("%s/api/v1/activitypub/user-id/15", distantURL)
		distantUser30URL := fmt.Sprintf("%s/api/v1/activitypub/user-id/30", distantURL)

		localUser := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		localUser2URL := localUrl.JoinPath("/api/v1/activitypub/user-id/2").String()
		localUser2Inbox := localUrl.JoinPath("/api/v1/activitypub/user-id/2/inbox").String()

		ctx, _ := contexttest.MockAPIContext(t, localUser2Inbox)

		// Distant user 15 initiates federeted user 15 following local user 2 (should pass)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		distantURI, err := url.Parse(distantURL)
		require.NoError(t, err)

		c_user_15, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey, mock.Persons[0].KeyID(federatedSrv.URL), []*url.URL{distantURI})
		require.NoError(t, err)

		//------------- Test keyID not equal to ActorID -------------
		follow_false := fmt.Appendf(
			nil,
			`{"type":"Follow",`+
				`"actor":"%s",`+
				`"object":"%s"}`,
			distantUser30URL,
			localUser2URL,
		)

		resp_false, err := c_user_15.Post(follow_false, localUser2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotAcceptable, resp_false.StatusCode)

		// follow does not exist
		distantFederatedUser30 := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "30"})
		unittest.AssertNotExistsBean(t,
			&user_model.FederatedUserFollower{
				FollowedUserID:  localUser.ID,
				FollowingUserID: distantFederatedUser30.UserID,
			},
		)

		//------------- Test keyID equal to ActorID -------------
		follow_true := fmt.Appendf(
			nil,
			`{"type":"Follow",`+
				`"actor":"%s",`+
				`"object":"%s"}`,
			distantUser15URL,
			localUser2URL,
		)

		resp_true_user, err := c_user_15.Post(follow_true, localUser2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusAccepted, resp_true_user.StatusCode)

		// local follow exists
		distantFederatedUser15 := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "15"})
		unittest.AssertExistsAndLoadBean(t,
			&user_model.FederatedUserFollower{
				FollowedUserID:  localUser.ID,
				FollowingUserID: distantFederatedUser15.UserID,
			},
		)

	})

}
