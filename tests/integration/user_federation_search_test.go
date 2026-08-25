// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/forgefed"
	"forgejo.org/models/unittest"
	"forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/federation"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRemoteUserHandle(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	mockFederationAllowAllHosts(t)
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	ctx := t.Context()

	// 1. Resolve via direct ActivityPub actor URL
	distantUser15URL := fmt.Sprintf("%s/api/v1/activitypub/user-id/15", federatedSrv.URL)
	u, err := federation.ResolveRemoteUserHandle(ctx, distantUser15URL)
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.True(t, u.IsActivityPub())

	// Verify database record
	federationHost := unittest.AssertExistsAndLoadBean(t, &forgefed.FederationHost{HostFqdn: "127.0.0.1"})
	unittest.AssertExistsAndLoadBean(t, &user.FederatedUser{ExternalID: "15", FederationHostID: federationHost.ID})

	// 2. Search via explore user search web route
	session := loginUser(t, "user1")
	req := NewRequest(t, "GET", fmt.Sprintf("/explore/users?q=%s", url.QueryEscape(distantUser15URL)))
	resp := session.MakeRequest(t, req, http.StatusOK)
	assert.Contains(t, resp.Body.String(), u.DisplayName())
}
