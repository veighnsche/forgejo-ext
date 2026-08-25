// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActivityPubPersonInboxIgnoredActivities verifies that common Fediverse
// activities Forgejo does not model (Announce/boost, Delete, Update) are
// accepted gracefully with 204 instead of being rejected with 406, so peers
// like Mastodon and GoToSocial do not get errors when delivering them.
func TestActivityPubPersonInboxIgnoredActivities(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		localUser2Inbox := u.JoinPath("/api/v1/activitypub/user-id/2/inbox").String()

		ctx, _ := contexttest.MockAPIContext(t, localUser2Inbox)
		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		c, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL), nil)
		require.NoError(t, err)

		for _, activityType := range []string{"Announce", "Delete", "Update"} {
			activity := fmt.Appendf(nil,
				`{"type":"%s",`+
					`"actor":"%s/api/v1/activitypub/user-id/15",`+
					`"object":"https://example.com/objects/1"}`,
				activityType, federatedSrv.URL)
			resp, err := c.Post(activity, localUser2Inbox)
			require.NoError(t, err)
			assert.Equal(t, http.StatusNoContent, resp.StatusCode,
				"%s must be accepted gracefully", activityType)
		}
	})
}
