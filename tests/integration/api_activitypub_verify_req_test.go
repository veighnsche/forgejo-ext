// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
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

	"github.com/42wim/httpsig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// ActivityStreamsContentType const
	ActivityStreamsContentType = `application/ld+json; profile="https://www.w3.org/ns/activitystreams"`
	httpsigExpirationTime      = 60
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

		cf, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		distantURI, err := url.Parse(distantURL)
		require.NoError(t, err)

		cUser15, err := cf.WithKeysDirect(ctx, mock.Persons[0].PrivKey, mock.Persons[0].KeyID(federatedSrv.URL), []*url.URL{distantURI})
		require.NoError(t, err)

		//------------- Test keyID not equal to ActorID -------------
		t.Run("keyIDNotEqualActorID", func(t *testing.T) {
			followFalse := fmt.Appendf(
				nil,
				`{"type":"Follow",`+
					`"actor":"%s",`+
					`"object":"%s"}`,
				distantUser30URL,
				localUser2URL,
			)

			respFalse, err := cUser15.Post(followFalse, localUser2Inbox)
			require.NoError(t, err)
			assert.Equal(t, http.StatusUnauthorized, respFalse.StatusCode)

			// follow does not exist
			distantFederatedUser30 := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "30"})
			unittest.AssertNotExistsBean(t,
				&user_model.FederatedUserFollower{
					FollowedUserID:  localUser.ID,
					FollowingUserID: distantFederatedUser30.UserID,
				},
			)
		})
		//------------- Test keyID equal to ActorID -------------
		t.Run("keyIDAEqualActorID", func(t *testing.T) {
			followTrue := fmt.Appendf(
				nil,
				`{"type":"Follow",`+
					`"actor":"%s",`+
					`"object":"%s"}`,
				distantUser15URL,
				localUser2URL,
			)

			respTrueUser, err := cUser15.Post(followTrue, localUser2Inbox)
			require.NoError(t, err)
			assert.Equal(t, http.StatusAccepted, respTrueUser.StatusCode)

			// local follow exists
			distantFederatedUser15 := unittest.AssertExistsAndLoadBean(t, &user_model.FederatedUser{ExternalID: "15"})
			unittest.AssertExistsAndLoadBean(t,
				&user_model.FederatedUserFollower{
					FollowedUserID:  localUser.ID,
					FollowingUserID: distantFederatedUser15.UserID,
				},
			)
		})
	})
}

func TestActivityPubVeryfiyReqDigest(t *testing.T) {
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
		localUser2Inbox := localUrl.JoinPath("/api/v1/activitypub/user-id/2/inbox").String()

		followActivity := fmt.Appendf(
			nil,
			`{"type":"Follow",`+
				`"actor":"%s",`+
				`"object":"%s"}`,
			mock.Persons[0].KeyID(federatedSrv.URL),
			localUser2Inbox,
		)
		req, err := createPostReq(
			followActivity,
			mock.Persons[0].PrivKey,
			mock.Persons[0].KeyID(federatedSrv.URL),
			localUser2Inbox,
		)
		require.NoError(t, err)

		t.Run("validRequest", func(t *testing.T) {
			MakeRequest(t, &RequestWrapper{req}, http.StatusAccepted)
		})

		t.Run("invalidRequest", func(t *testing.T) {
			forgedBody := fmt.Appendf(
				nil,
				`{"type":"Follow",`+
					`"actor":"%s",`+
					`"object":"%s"}`,
				"someserver.com/api/v1/activiypup/user-id/1457",
				"/api/v1/activitypub/user-id/2/inbox",
			)
			require.NoError(t, err)

			req.Body = io.NopCloser(bytes.NewReader(forgedBody))

			MakeRequest(t, &RequestWrapper{req}, http.StatusBadRequest)
		})
	})
}

func createPostReq(body []byte, privateKey, pubID, to string) (req *http.Request, err error) {
	privPem, _ := pem.Decode([]byte(privateKey))
	privParsed, err := x509.ParsePKCS1PrivateKey(privPem.Bytes)
	if err != nil {
		return nil, err
	}

	algs := setting.HttpsigAlgs
	digestAlg := httpsig.DigestAlgorithm(setting.Federation.DigestAlgorithm)
	postHeaders := setting.Federation.PostHeaders

	buf := bytes.NewBuffer(body)
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
		if err := signer.SignRequest(privParsed, pubID, req, body); err != nil {
			return nil, err
		}
	}
	return req, err
}
