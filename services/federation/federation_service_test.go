// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"fmt"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	ap "github.com/go-ap/activitypub"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m, &unittest.TestOptions{})
}

func TestVerifyKeyIDMatchesActorID(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.SignatureEnforced, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&setting.Federation.MaxSize, 2048)()

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	_, err := FindOrCreateActorKey(t.Context(), mock.ApActor.KeyID(federatedSrv.URL))
	require.NoError(t, err)

	actor := ap.Actor{ID: ap.IRI(mock.Persons[0].KeyID(federatedSrv.URL))}
	activity := ap.Activity{Actor: actor}

	followActivity := fmt.Appendf(
		nil,
		`{"type":"Follow",`+
			`"actor":"%s",`+
			`"object":"%s"}`,
		mock.Persons[0].KeyID(federatedSrv.URL),
		"/api/v1/activitypub/user-id/2/inbox",
	)

	t.Run("valid_signed_with_user_key", func(t *testing.T) {
		req, err := test.CreateFederationPostReq(
			followActivity,
			mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL),
			"/api/v1/activitypub/user-id/2/inbox",
		)
		require.NoError(t, err)

		err = VerifyKeyIDMatchesActorID(t.Context(), req, &activity)
		require.NoError(t, err)
	})

	t.Run("valid_signed_with_host_key", func(t *testing.T) {
		req, err := test.CreateFederationPostReq(
			followActivity,
			mock.ApActor.PrivKey,
			mock.ApActor.KeyID(federatedSrv.URL),
			"/api/v1/activitypub/user-id/2/inbox",
		)
		require.NoError(t, err)

		err = VerifyKeyIDMatchesActorID(t.Context(), req, &activity)
		require.NoError(t, err)
	})

	t.Run("invalid_request", func(t *testing.T) {
		req, err := test.CreateFederationPostReq(
			followActivity,
			mock.Persons[1].PrivKey,
			mock.Persons[1].KeyID(federatedSrv.URL),
			"/api/v1/activitypub/user-id/2/inbox",
		)
		require.NoError(t, err)

		err = VerifyKeyIDMatchesActorID(t.Context(), req, &activity)
		require.Error(t, err)
	})
}
