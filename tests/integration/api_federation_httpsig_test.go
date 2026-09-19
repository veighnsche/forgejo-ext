// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"database/sql"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/forgefed"
	"forgejo.org/models/unittest"
	"forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/federation"
	"forgejo.org/tests/forgery"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFederationHttpSigValidation(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	err := federation.Init()
	require.NoError(t, err)

	mock := test.NewFederationServerMock()
	federatedSrv := mock.DistantServer(t)
	federatedSrvKeyID := fmt.Sprintf("%s/api/v1/activitypub/actor#main-key", federatedSrv.URL)
	followUser := mock.Persons[0]
	followURL := followUser.FederationID(federatedSrv.URL)
	followKeyID := followUser.KeyID(federatedSrv.URL)

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		userID := 2
		userURL := fmt.Sprintf("%sapi/v1/activitypub/user-id/%d", u, userID)

		user1 := unittest.AssertExistsAndLoadBean(t, &user.User{ID: 1})

		ctx, _ := contexttest.MockAPIContext(t, userURL)
		clientFactory, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		apClient, err := clientFactory.WithKeys(ctx, user1, user1.KeyID(), nil)
		require.NoError(t, err)

		// HACK HACK HACK: the host part of the URL gets set to which IP forgejo is
		// listening on, NOT localhost, which is the Domain given to forgejo which
		// is then used for eg. the keyID all requests
		applicationKeyID := fmt.Sprintf("%sapi/v1/activitypub/actor#main-key", setting.AppURL)
		actorKeyID := fmt.Sprintf("%sapi/v1/activitypub/user-id/1#main-key", setting.AppURL)

		appURL, err := url.Parse(setting.AppURL)
		require.NoError(t, err)
		followClient, err := clientFactory.WithKeysDirect(ctx, followUser.PrivKey, followKeyID, []*url.URL{appURL})
		require.NoError(t, err)

		federatedURL, err := url.Parse(federatedSrv.URL)
		require.NoError(t, err)

		federatedPort, err := strconv.ParseUint(federatedURL.Port(), 10, 16)
		require.NoError(t, err)

		federatedHost, err := forgefed.NewFederationHost(
			federatedSrv.URL,
			forgefed.NodeInfo{SoftwareName: forgefed.ForgejoSourceType},
			uint16(federatedPort),
			federatedURL.Scheme,
		)
		require.NoError(t, err)

		federatedHost.KeyID = sql.NullString{
			String: federatedSrvKeyID,
			Valid:  true,
		}

		mockPubBlock, _ := pem.Decode([]byte(mock.ApActor.PubKey))

		federatedHost.PublicKey = sql.Null[sql.RawBytes]{
			V:     mockPubBlock.Bytes,
			Valid: true,
		}

		err = forgefed.CreateFederationHost(ctx, &federatedHost)
		require.NoError(t, err)

		federatedClient, err := clientFactory.WithKeysDirect(ctx, mock.ApActor.PrivKey, federatedSrvKeyID, []*url.URL{appURL})
		require.NoError(t, err)

		// Unsigned request
		t.Run("UnsignedRequest", func(t *testing.T) {
			req := NewRequest(t, "GET", userURL)
			MakeRequest(t, req, http.StatusBadRequest)
		})

		// Check for missing public keys
		t.Run("ValidateEmptyCaches", func(t *testing.T) {
			_, err := forgefed.FindFederationHostByKeyID(db.DefaultContext, applicationKeyID)
			require.Error(t, err)
			assert.True(t, forgefed.IsErrFederationHostNotFound(err))

			_, _, err = user.FindFederatedUserByKeyID(db.DefaultContext, actorKeyID)
			require.Error(t, err)
			assert.True(t, user.IsErrFederatedUserNotExists(err))
		})

		// Signed CAVAGE GET request
		t.Run("SignedGetCAVAGERequest", func(t *testing.T) {
			assert.False(t, apClient.GetRFC9421())

			req, err := apClient.GetRequest(userURL)
			require.NoError(t, err)

			sig := req.Header.Get("Signature")

			assert.NotEmpty(t, sig)
			assert.Contains(t, sig, `algorithm="hs2019"`)

			expKeyID := fmt.Sprintf(`keyId="%v"`, apClient.KeyID())
			assert.Contains(t, sig, expKeyID)

			expHeaders := fmt.Sprintf(`headers="%v"`, apClient.SignedHeaders(http.MethodGet, false))
			assert.Contains(t, sig, expHeaders)

			assert.Contains(t, sig, "signature=")

			resp, err := apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			resp, err = apClient.Get(userURL)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			resp, err = apClient.Get(followURL)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		// Signed CAVAGE GET request (failure tests)
		t.Run("SignedGetCAVAGERequestFailures", func(t *testing.T) {
			assert.False(t, apClient.GetRFC9421())

			req, err := apClient.GetRequest(userURL)
			require.NoError(t, err)

			sig := req.Header.Get("Signature")

			assert.NotEmpty(t, sig)
			assert.Contains(t, sig, `algorithm="hs2019"`)

			// empty signature
			req.Header.Set("Signature", "")
			resp, err := apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			// empty algorithm
			re := regexp.MustCompile(`algorithm="[a-zA-Z0-9_\-]*"`)
			badSigAlg := re.ReplaceAllString(sig, ``)
			req.Header.Set("Signature", badSigAlg)
			resp, err = apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			// bad key ID
			re = regexp.MustCompile(`keyId=".*"`)
			badKeyID := re.ReplaceAllString(sig, `keyId="https://bad.key/id#main"`)
			req.Header.Set("Signature", badKeyID)
			resp, err = apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			// bad signature
			re = regexp.MustCompile(`signature=".*"`)
			badSig := re.ReplaceAllString(sig, `signature="badSignature"`)
			req.Header.Set("Signature", badSig)
			resp, err = apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		// Signed RFC 9421 GET request
		t.Run("SignedGetRFC9421Request", func(t *testing.T) {
			apClient.SetRFC9421(true)
			federatedClient.SetRFC9421(true)
			assert.True(t, apClient.GetRFC9421())

			req, err := apClient.GetRequest(userURL)
			require.NoError(t, err)

			_, err = activitypub.VerifyPubKeyRFC9421(req, activitypub.ClientKeyUser)
			require.NoError(t, err)

			sigInput := req.Header.Get("Signature-Input")
			sig := req.Header.Get("Signature")

			assert.NotEmpty(t, sigInput)
			assert.NotEmpty(t, sig)

			assert.Contains(t, sigInput, `alg="rsa-v1_5-sha256"`)

			expKeyID := fmt.Sprintf(`keyid="%v"`, apClient.KeyID())
			assert.Contains(t, sigInput, expKeyID)

			expHeaders := fmt.Sprintf(`sig1=(%v)`, apClient.SignedHeaders(http.MethodGet, false))
			assert.Contains(t, sigInput, expHeaders)

			_, err = activitypub.VerifyPubKeyRFC9421(req, activitypub.ClientKeyUser)
			require.NoError(t, err)

			resp, err := apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			resp, err = apClient.Get(userURL)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			resp, err = apClient.Get(followURL)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			req, err = federatedClient.GetRequest(userURL)
			require.NoError(t, err)

			_, err = activitypub.VerifyPubKeyRFC9421(req, activitypub.ClientKeyHost)
			require.NoError(t, err, "request headers: %v", req.Header)

			resp, err = federatedClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			resp, err = federatedClient.Get(followURL)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		// Signed RFC 9421 GET request (failure tests)
		t.Run("SignedGetRFC9421RequestFailures", func(t *testing.T) {
			apClient.SetRFC9421(true)
			assert.True(t, apClient.GetRFC9421())

			req, err := apClient.GetRequest(userURL)
			require.NoError(t, err)

			sig := req.Header.Get("Signature")
			sigInput := req.Header.Get("Signature-Input")

			assert.NotEmpty(t, sig)
			assert.NotEmpty(t, sigInput)

			// assert valid GET request
			resp, err := apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// empty signature
			req.Header.Set("Signature", "")
			resp, err = apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			// empty algorithm
			re := regexp.MustCompile(`alg="[a-zA-Z0-9_\-]*"`)
			badSigAlg := re.ReplaceAllString(sigInput, ``)
			req.Header.Set("Signature-Input", badSigAlg)
			resp, err = apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			// bad key ID
			re = regexp.MustCompile(`keyid=".*"`)
			badKeyID := re.ReplaceAllString(sig, `keyid="https://bad.key/id#main"`)
			req.Header.Set("Signature-Input", badKeyID)
			resp, err = apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			// bad signature
			req.Header.Set("Signature", "badSignature")
			resp, err = apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		// Signed CAVAGE POST request
		t.Run("SignedPostCAVAGERequest", func(t *testing.T) {
			apClient.SetRFC9421(false)
			followClient.SetRFC9421(false)

			assert.False(t, apClient.GetRFC9421())
			assert.False(t, followClient.GetRFC9421())

			followActivity, err := fm.NewForgeFollow(followURL, userURL)
			require.NoError(t, err)

			followJSON, err := followActivity.MarshalJSON()
			require.NoError(t, err)

			req, err := followClient.PostRequest(followJSON, fmt.Sprintf("%v/inbox", userURL))
			require.NoError(t, err)

			sig := req.Header.Get("Signature")

			assert.NotEmpty(t, sig)
			assert.Contains(t, sig, `algorithm="hs2019"`)

			expKeyID := fmt.Sprintf(`keyId="%v"`, followClient.KeyID())
			assert.Contains(t, sig, expKeyID)

			expHeaders := fmt.Sprintf(`headers="%v"`, followClient.SignedHeaders(http.MethodPost, true))
			assert.Contains(t, sig, expHeaders)

			assert.Contains(t, sig, "signature=")

			resp, err := followClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusAccepted, resp.StatusCode)

			resp, err = followClient.Post(followJSON, fmt.Sprintf("%v/inbox", followURL))
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		// Signed CAVAGE POST request (failure tests)
		t.Run("SignedPostCAVAGERequestFailures", func(t *testing.T) {
			apClient.SetRFC9421(false)
			followClient.SetRFC9421(false)

			assert.False(t, apClient.GetRFC9421())
			assert.False(t, followClient.GetRFC9421())

			followActivity, err := fm.NewForgeFollow(followURL, userURL)
			require.NoError(t, err)

			followJSON, err := followActivity.MarshalJSON()
			require.NoError(t, err)

			req, err := followClient.PostRequest(followJSON, fmt.Sprintf("%v/inbox", userURL))
			require.NoError(t, err)

			sig := req.Header.Get("Signature")

			assert.NotEmpty(t, sig)
			assert.Contains(t, sig, `algorithm="hs2019"`)

			// assert valid follow request
			resp, err := followClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusNoContent, resp.StatusCode)

			// empty signature
			req.Header.Set("Signature", "")
			resp, err = followClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			// empty algorithm
			re := regexp.MustCompile(`algorithm="[a-zA-Z0-9_\-]*"`)
			badSigAlg := re.ReplaceAllString(sig, ``)
			req.Header.Set("Signature", badSigAlg)
			_, err = followClient.Do(req)
			require.Error(t, err)

			// bad key ID
			re = regexp.MustCompile(`keyId=".*"`)
			badKeyID := re.ReplaceAllString(sig, `keyId="https://bad.key/id#main"`)
			req.Header.Set("Signature", badKeyID)
			_, err = followClient.Do(req)
			require.Error(t, err)

			// bad signature
			re = regexp.MustCompile(`signature=".*"`)
			badSig := re.ReplaceAllString(sig, `signature="badSignature"`)
			req.Header.Set("Signature", badSig)
			_, err = followClient.Do(req)
			require.Error(t, err)
		})

		// Signed RFC 9421 POST request
		t.Run("SignedPostRFC9421Request", func(t *testing.T) {
			apClient.SetRFC9421(true)
			followClient.SetRFC9421(true)

			assert.True(t, apClient.GetRFC9421())
			assert.True(t, followClient.GetRFC9421())

			followActivity, err := fm.NewForgeFollow(followURL, userURL)
			require.NoError(t, err)

			followJSON, err := followActivity.MarshalJSON()
			require.NoError(t, err)

			req, err := followClient.PostRequest(followJSON, fmt.Sprintf("%v/inbox", userURL))
			require.NoError(t, err)

			sigInput := req.Header.Get("Signature-Input")
			sig := req.Header.Get("Signature")

			assert.NotEmpty(t, sigInput)
			assert.NotEmpty(t, sig)

			assert.Contains(t, sigInput, `alg="rsa-v1_5-sha256"`)

			expKeyID := fmt.Sprintf(`keyid="%v"`, followClient.KeyID())
			assert.Contains(t, sigInput, expKeyID)

			expHeaders := fmt.Sprintf(`sig1=(%v)`, followClient.SignedHeaders(http.MethodPost, true))
			assert.Contains(t, sigInput, expHeaders)

			resp, err := followClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusNoContent, resp.StatusCode)

			resp, err = followClient.Post(followJSON, fmt.Sprintf("%v/inbox", followURL))
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		// Signed RFC 9421 POST request (failure tests)
		t.Run("SignedPostRFC9421RequestFailures", func(t *testing.T) {
			apClient.SetRFC9421(true)
			followClient.SetRFC9421(true)

			assert.True(t, apClient.GetRFC9421())
			assert.True(t, followClient.GetRFC9421())

			followActivity, err := fm.NewForgeFollow(followURL, userURL)
			require.NoError(t, err)

			activityJSON, err := followActivity.MarshalJSON()
			require.NoError(t, err)

			req, err := followClient.PostRequest(activityJSON, fmt.Sprintf("%v/inbox", userURL))
			require.NoError(t, err)

			// assert valid POST request
			resp, err := apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusNoContent, resp.StatusCode)

			sig := req.Header.Get("Signature")
			sigInput := req.Header.Get("Signature-Input")

			assert.NotEmpty(t, sig)

			// empty signature
			req.Header.Set("Signature", "")
			resp, err = apClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			// empty algorithm
			re := regexp.MustCompile(`alg="[a-zA-Z0-9_\-]*"`)
			badSigAlg := re.ReplaceAllString(sigInput, `alg=""`)
			req.Header.Set("Signature-Input", badSigAlg)
			_, err = apClient.Do(req)
			require.Error(t, err)

			// bad key ID
			re = regexp.MustCompile(`keyid=".*"`)
			badKeyID := re.ReplaceAllString(sig, `keyid="https://bad.key/id#main"`)
			req.Header.Set("Signature-Input", badKeyID)
			_, err = apClient.Do(req)
			require.Error(t, err)

			// bad signature
			req.Header.Set("Signature", "badSignature")
			_, err = apClient.Do(req)
			require.Error(t, err)
		})

		// Check for cached public keys
		t.Run("ValidateCaches", func(t *testing.T) {
			host, err := forgefed.FindFederationHostByKeyID(db.DefaultContext, applicationKeyID)
			require.NoError(t, err)
			assert.NotNil(t, host)
			assert.True(t, host.PublicKey.Valid)

			_, user, err := user.FindFederatedUserByKeyID(db.DefaultContext, actorKeyID)
			require.NoError(t, err)
			assert.NotNil(t, user)
			assert.True(t, user.PublicKey.Valid)
		})

		// Disable signature validation
		defer test.MockVariableValue(&setting.Federation.SignatureEnforced, false)()

		// Unsigned request
		t.Run("SignatureValidationDisabled", func(t *testing.T) {
			req := NewRequest(t, "GET", userURL)
			MakeRequest(t, req, http.StatusOK)
		})
	})
}

func TestFederationAllRoutesCovered(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	routes := routers.NormalRoutes().R.Routes()

	var r *chi.Route
	for _, route := range routes {
		if route.Pattern == "/api/v1/*" {
			r = &route
			break
		}
	}

	require.NotNil(t, r)

	ranOne := false
	for _, route := range r.SubRoutes.Routes() {
		if !strings.HasPrefix(route.Pattern, "/activitypub/") {
			continue
		}

		ranOne = true
		if route.Pattern == "/activitypub/actor" {
			// unsigned request to the actor should always succed
			req := NewRequest(t, "GET", fmt.Sprintf("%sapi/v1/activitypub/actor", setting.AppURL))
			MakeRequest(t, req, http.StatusOK)
		} else {
			// this just puts in something for the replacements to be able to make a request
			url := fmt.Sprintf("%sapi/v1%s", setting.AppURL, route.Pattern)
			for strings.Contains(url, "{") {
				before, after, _ := strings.Cut(url, "/{")
				_, after, _ = strings.Cut(after, "}/")
				url = fmt.Sprintf("%s/1/%s", before, after)
			}

			var req *RequestWrapper
			if strings.Contains(route.Pattern, "inbox") {
				req = NewRequestWithJSON(t, "POST", url, "{}")
			} else {
				req = NewRequest(t, "GET", url)
			}

			resp := MakeRequest(t, req, http.StatusBadRequest)
			assert.Contains(t, resp.Body.String(), "request signature verification failed")
		}
	}

	require.True(t, ranOne)
}

func TestFederationHttpSigValidationRoundtrip(t *testing.T) {
	defer test.MockVariableValue(&setting.Federation.Enabled, true)()
	defer test.MockVariableValue(&setting.Federation.SignatureEnforced, true)()
	defer test.MockVariableValue(&setting.Federation.InsecureAllowInvalidHosts, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	err := federation.Init()
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Logf("RFC9421 request: %v\n", req)
		t.Logf("Created header: %v\n", req.Header.Get("created"))
		_, err = activitypub.VerifyPubKeyRFC9421(req, activitypub.ClientKeyUser)
		require.NoError(t, err)
		w.Write([]byte("OK"))
	}))
	defer srv.Close()

	srvURL, err := url.Parse(srv.URL)
	require.NoError(t, err)

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		clientFactory, err := activitypub.NewClientFactoryWithTimeout(60 * time.Second)
		require.NoError(t, err)

		ctx := t.Context()

		user, _ := forgery.CreateFederatedUser(t, nil)

		apClient, err := clientFactory.WithKeys(ctx, user, user.KeyID(), []*url.URL{srvURL})
		require.NoError(t, err)

		apClient.SetRFC9421(true)
		assert.True(t, apClient.GetRFC9421())

		_, err = apClient.Get(srv.URL)
		require.NoError(t, err)
	})
}
