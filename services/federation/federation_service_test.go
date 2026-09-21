// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"github.com/42wim/httpsig"
	ap "github.com/go-ap/activitypub"
	"github.com/stretchr/testify/require"
)

const (
	// ActivityStreamsContentType const
	ActivityStreamsContentType = `application/ld+json; profile="https://www.w3.org/ns/activitystreams"`
	httpsigExpirationTime      = 60
)

func TestMain(m *testing.M) {
	unittest.MainTest(m, &unittest.TestOptions{
		FixtureFiles: []string{
			"user.yml",
			"federation_host.yml",
			"federated_user.yml",
		},
	})
}

func TestVerifyKeyIDMatchesActorID(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Context()
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.SignatureEnforced, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&setting.Federation.MaxSize, 2048)()

	// setup
	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	defer federatedSrv.Close()

	_, err := FindOrCreateActorKey(t.Context(), mock.ApActor.KeyID(federatedSrv.URL))
	require.NoError(t, err)

	actor := ap.Actor{ID: ap.IRI(mock.Persons[0].KeyID(federatedSrv.URL))}
	activity := ap.Activity{Actor: actor}

	follow_activity := fmt.Appendf(
		nil,
		`{"type":"Follow",`+
			`"actor":"%s",`+
			`"object":"%s"}`,
		mock.Persons[0].KeyID(federatedSrv.URL),
		"/api/v1/activitypub/user-id/2/inbox",
	)

	t.Run("valid_signed_with_user_key", func(t *testing.T) {
		req, err := createPostReq(
			follow_activity,
			mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL),
			"/api/v1/activitypub/user-id/2/inbox")
		require.NoError(t, err)

		err = VerifyKeyIDMatchesActorID(t.Context(), req, &activity)
		require.NoError(t, err)
	})

	t.Run("valid_signed_with_host_key", func(t *testing.T) {
		req, err := createPostReq(
			follow_activity,
			mock.ApActor.PrivKey,
			mock.ApActor.KeyID(federatedSrv.URL),
			"/api/v1/activitypub/user-id/2/inbox")
		require.NoError(t, err)

		err = VerifyKeyIDMatchesActorID(t.Context(), req, &activity)
		require.NoError(t, err)
	})

	t.Run("invalid_request", func(t *testing.T) {
		req, err := createPostReq(
			follow_activity,
			mock.Persons[1].PrivKey,
			mock.Persons[1].KeyID(federatedSrv.URL),
			"/api/v1/activitypub/user-id/2/inbox")
		require.NoError(t, err)

		err = VerifyKeyIDMatchesActorID(t.Context(), req, &activity)
		require.Error(t, err)
	})
}

func createPostReq(b []byte, privateKey string, pubID string, to string) (req *http.Request, err error) {

	privPem, _ := pem.Decode([]byte(privateKey))
	privParsed, err := x509.ParsePKCS1PrivateKey(privPem.Bytes)

	algs := setting.HttpsigAlgs
	digestAlg := httpsig.DigestAlgorithm(setting.Federation.DigestAlgorithm)
	postHeaders := setting.Federation.PostHeaders

	buf := bytes.NewBuffer(b)
	req, err = http.NewRequest(http.MethodPost, to, buf)

	if err != nil {
		return nil, err
	}

	req.Header.Add("Accept", "application/json, "+ActivityStreamsContentType)
	req.Header.Add("Date", "Mon, 21 Sep 2026 08:56:24 GMT")
	req.Header.Add("Host", req.URL.Host)
	req.Header.Add("User-Agent", "Gitea/"+setting.AppVer)
	req.Header.Add("Content-Type", ActivityStreamsContentType)

	if pubID != "" {
		signer, _, err := httpsig.NewSigner(algs, digestAlg, postHeaders, httpsig.Signature, httpsigExpirationTime)
		if err != nil {
			return nil, err
		}
		if err := signer.SignRequest(privParsed, pubID, req, b); err != nil {
			return nil, err
		}
	}
	return req, err
}
