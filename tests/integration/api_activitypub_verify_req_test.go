// Copyright 2024 The Forgejo Authors. All rights reserved.
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		assert.Equal(t, http.StatusUnauthorized, resp_false.StatusCode)

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
