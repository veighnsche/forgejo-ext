// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/tests"

	ap "github.com/go-ap/activitypub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivityPubActor(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()
	defer tests.PrepareTestEnv(t)()

	req := NewRequest(t, "GET", "/api/v1/activitypub/actor")
	resp := MakeRequest(t, req, http.StatusOK)
	assert.Contains(t, resp.Body.String(), "@context")

	var actor ap.Actor
	err := actor.UnmarshalJSON(resp.Body.Bytes())
	require.NoError(t, err)

	assert.Equal(t, ap.ApplicationType, actor.Type)
	assert.Equal(t, "ghost", actor.PreferredUsername.String())
	keyID := actor.GetID().String()
	assert.Regexp(t, "activitypub/actor$", keyID)
	assert.Regexp(t, "activitypub/actor/inbox$", actor.Inbox.GetID().String())
	assert.Regexp(t, "activitypub/actor/outbox$", actor.Outbox.GetID().String())

	pubKey := actor.PublicKey
	assert.NotNil(t, pubKey)
	publicKeyID := keyID + "#main-key"
	assert.Equal(t, pubKey.ID.String(), publicKeyID)

	pubKeyPem := pubKey.PublicKeyPem
	assert.NotNil(t, pubKeyPem)
	assert.Regexp(t, "^-----BEGIN PUBLIC KEY-----", pubKeyPem)

	t.Run("ActorOutboxUnsignedRejected", func(t *testing.T) {
		// /inbox and /outbox routes also require signature checks
		req := NewRequest(t, "GET", actor.Outbox.GetID().String())
		resp := MakeRequest(t, req, http.StatusBadRequest)
		assert.Contains(t, resp.Body.String(), "request signature verification failed")
	})
}
