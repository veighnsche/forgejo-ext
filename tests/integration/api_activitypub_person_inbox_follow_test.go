// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/federation"

	ap "github.com/go-ap/activitypub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Flow of this test is documented at: https://codeberg.org/forgejo-contrib/federation/src/branch/main/doc/user-activity-following.md
func TestActivityPubPersonInboxFollow(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
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
		distantUser15AliasURL := fmt.Sprintf("%s/api/v1/activitypub/user-id/alias15", distantURL)

		localUser := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		localUser2URL := localUrl.JoinPath("/api/v1/activitypub/user-id/2").String()
		localUser2Inbox := localUrl.JoinPath("/api/v1/activitypub/user-id/2/inbox").String()

		ctx, _ := contexttest.MockAPIContext(t, localUser2Inbox)

		// distant follows local
		followActivity := fmt.Appendf(nil,
			`{"type":"Follow",`+
				`"actor":"%s",`+
				`"object":"%s"}`,
			distantUser15AliasURL,
			localUser2URL,
		)

		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		distantURI, err := url.Parse(distantURL)
		require.NoError(t, err)

		// Sign as distant person 15, the actor of the activity.
		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey, mock.Persons[0].KeyID(federatedSrv.URL), []*url.URL{distantURI})
		require.NoError(t, err)

		resp, err := c.Post(followActivity, localUser2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)

		// local follow exists
		distantFederatedUser := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "15"})
		unittest.AssertExistsAndLoadBean(t,
			&user_model.FederatedUserFollower{
				FollowedUserID:  localUser.ID,
				FollowingUserID: distantFederatedUser.UserID,
			},
		)

		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: distantFederatedUser.UserID})
		assert.Equal(t, user_model.UserTypeActivityPubUser, user.Type)
		assert.True(t, user.ProhibitLogin)
		assert.Empty(t, user.Passwd)
		assert.Empty(t, user.PasswdHashAlgo)
		assert.Empty(t, user.Salt)

		// distant is informed about accepting follow
		assert.Contains(t, mock.LastPost, "\"type\":\"Accept\"")

		// distant undoes follow
		undoFollowActivity := fmt.Appendf(nil,
			`{"type":"Undo",`+
				`"actor":"%s",`+
				`"object":{"type":"Follow",`+
				`"actor":"%s",`+
				`"object":"%s"}}`,
			distantUser15URL,
			distantUser15URL,
			localUser2URL,
		)

		c, err = cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)

		require.NoError(t, err)

		resp, err = c.Post(undoFollowActivity, localUser2Inbox)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		// local follow removed
		unittest.AssertNotExistsBean(t,
			&user_model.FederatedUserFollower{
				FollowedUserID:  localUser.ID,
				FollowingUserID: distantFederatedUser.UserID,
			},
		)
	})
}

func TestActivityPubFollowRefollow(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	require.NoError(t, federation.Init())

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, localUrl *url.URL) {
		defer test.MockVariableValue(&setting.AppURL, localUrl.String())()

		distantURL := federatedSrv.URL
		distantUser15AliasURL := fmt.Sprintf("%s/api/v1/activitypub/user-id/alias15", distantURL)

		localUser := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		localUser2URL := localUrl.JoinPath("/api/v1/activitypub/user-id/2")
		localUser2Inbox := localUrl.JoinPath("/api/v1/activitypub/user-id/2/inbox")

		ctx := t.Context()

		var follow user_model.FederatedUserFollower
		has, err := db.GetEngine(ctx).Get(&follow)
		require.NoError(t, err)
		assert.False(t, has)

		// distant follows local, signed as distant person 15
		apiCtx, _ := contexttest.MockAPIContext(t, localUser2Inbox.String())
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)
		distantURI, err := url.Parse(distantURL)
		require.NoError(t, err)
		c, err := cf.WithKeysDirect(apiCtx, mock.Persons[0].PrivKey, mock.Persons[0].KeyID(federatedSrv.URL), []*url.URL{distantURI})
		require.NoError(t, err)
		followActivity := fmt.Appendf(nil,
			`{"type":"Follow","actor":"%s/api/v1/activitypub/user-id/15","object":"%s"}`,
			distantURL, localUser2URL)
		resp, err := c.Post(followActivity, localUser2Inbox.String())
		require.NoError(t, err)
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)

		has, err = db.GetEngine(ctx).Get(&follow)
		require.NoError(t, err)
		assert.True(t, has)
		assert.Equal(t, int64(2), follow.FollowedUserID)

		err = federation.FollowRemoteActor(apiCtx.Base, localUser, distantUser15AliasURL)
		require.NoError(t, err)
	})
}

// TestActivityPubPersonOutboundUnfollow verifies that unfollowing a remote
// actor sends an Undo(Follow) activity to its inbox, keeping follow state
// consistent between instances. It mirrors FollowRemoteActor.
func TestActivityPubPersonOutboundUnfollow(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	federation.Init()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, localUrl *url.URL) {
		defer test.MockVariableValue(&setting.AppURL, localUrl.String())()

		distantUser15URL := fmt.Sprintf("%s/api/v1/activitypub/user-id/15", federatedSrv.URL)
		localUser := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

		apiCtx, _ := contexttest.MockAPIContext(t, localUrl.JoinPath("/api/v1/activitypub/user-id/2/inbox").String())

		// 1. Follow the distant actor; the distant server records the Follow.
		err := federation.FollowRemoteActor(apiCtx.Base, localUser, distantUser15URL)
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			return strings.Contains(mock.LastPost, "\"type\":\"Follow\"")
		}, 5*time.Second, 100*time.Millisecond, "expected the distant server to receive a Follow")

		// The distant actor must be materialised locally before unfollowing.
		unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "15"})

		// 2. Unfollow the distant actor; the distant server must receive Undo(Follow).
		err = federation.UnfollowRemoteActor(apiCtx.Base, localUser, distantUser15URL)
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			return strings.Contains(mock.LastPost, "\"type\":\"Undo\"")
		}, 5*time.Second, 100*time.Millisecond, "expected the distant server to receive an Undo")

		// The Undo must wrap a Follow for the same target.
		var undo fm.ForgeUndoFollow
		require.NoError(t, undo.UnmarshalJSON([]byte(mock.LastPost)))
		require.Equal(t, ap.UndoType, undo.Type)
		inner, ok := undo.Object.(*ap.Activity)
		require.True(t, ok, "Undo object must be a nested activity")
		require.Equal(t, ap.FollowType, inner.Type)
		require.Contains(t, inner.Object.GetID().String(), "/api/v1/activitypub/user-id/15")
	})
}
