// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package hcaptcha

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	dummySiteKey = "10000000-ffff-ffff-ffff-000000000001"
	dummySecret  = "0x0000000000000000000000000000000000000000"
	dummyToken   = "10000000-aaaa-bbbb-cccc-000000000001"
)

type captchaTransport func(*http.Request) (*http.Response, error)

func (transport captchaTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestCaptcha(t *testing.T) {
	for _, tc := range []struct {
		name       string
		secret     string
		token      string
		opts       PostOptions
		form       url.Values
		body       string
		response   Response
		decodeFail bool
	}{
		{
			name: "Success", secret: dummySecret, token: dummyToken,
			opts: PostOptions{Sitekey: dummySiteKey},
			form: url.Values{"secret": {dummySecret}, "response": {dummyToken}, "sitekey": {dummySiteKey}},
			body: `{"success":true,"challenge_ts":"2026-10-23T12:00:00Z","hostname":"forgejo.example","credit":true}`,
			response: Response{
				Success: true, ChallengeTS: "2026-10-23T12:00:00Z", Hostname: "forgejo.example", Credit: true,
			},
		},
		{
			name: "Encoded values and remote IP", secret: "secret + /?=&", token: "token + /?=&",
			opts: PostOptions{Sitekey: dummySiteKey, RemoteIP: "2001:db8::1"},
			form: url.Values{"secret": {"secret + /?=&"}, "response": {"token + /?=&"}, "sitekey": {dummySiteKey}, "remoteip": {"2001:db8::1"}},
			body: `{"success":true}`, response: Response{Success: true},
		},
		{
			name: "Blank optional fields are omitted", secret: dummySecret, token: dummyToken,
			opts: PostOptions{Sitekey: " \t", RemoteIP: " \t"},
			form: url.Values{"secret": {dummySecret}, "response": {dummyToken}},
			body: `{"success":true}`, response: Response{Success: true},
		},
		{
			name: "Invalid Token", secret: dummySecret, token: "invalid",
			opts: PostOptions{Sitekey: dummySiteKey},
			form: url.Values{"secret": {dummySecret}, "response": {"invalid"}, "sitekey": {dummySiteKey}},
			body: `{"success":false,"error-codes":["invalid-input-response"]}`,
			response: Response{ErrorCodes: []ErrorCode{
				ErrInvalidInputResponse,
			}},
		},
		{
			name: "Malformed response", secret: dummySecret, token: dummyToken,
			form: url.Values{"secret": {dummySecret}, "response": {dummyToken}},
			body: `{"success":`, decodeFail: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			client, err := New(tc.secret, WithHTTP(&http.Client{Transport: captchaTransport(func(request *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, "https://hcaptcha.com/siteverify", request.URL.String())
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				require.Equal(t, tc.form.Encode(), string(body))
				form, err := url.ParseQuery(string(body))
				require.NoError(t, err)
				require.Equal(t, tc.form, form)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(tc.body)),
				}, nil
			})}))
			require.NoError(t, err)
			response, err := client.Verify(tc.token, tc.opts)
			if tc.decodeFail {
				require.Error(t, err)
				require.Nil(t, response)
			} else {
				require.NoError(t, err)
				require.Equal(t, &tc.response, response)
			}
			require.Equal(t, 1, requests)
		})
	}
}

func TestCaptchaMissingInput(t *testing.T) {
	requests := 0
	option := WithHTTP(&http.Client{Transport: captchaTransport(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected network request")
	})})
	for _, secret := range []string{"", " \t"} {
		client, err := New(secret, option)
		require.ErrorIs(t, err, ErrMissingInputSecret)
		require.Nil(t, client)
	}
	client, err := New(dummySecret, option)
	require.NoError(t, err)
	for _, token := range []string{"", " \t"} {
		response, err := client.Verify(token, PostOptions{Sitekey: dummySiteKey})
		require.ErrorIs(t, err, ErrMissingInputResponse)
		require.Nil(t, response)
	}
	require.Zero(t, requests, "missing input must be rejected before any request")
}

func TestCaptchaTransportError(t *testing.T) {
	transportErr := errors.New("verification transport failed")
	client, err := New(dummySecret, WithHTTP(&http.Client{Transport: captchaTransport(func(*http.Request) (*http.Response, error) {
		return nil, transportErr
	})}))
	require.NoError(t, err)
	response, err := client.Verify(dummyToken, PostOptions{})
	require.ErrorIs(t, err, transportErr)
	require.Nil(t, response)
}

func TestCaptchaContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client, err := New(dummySecret, WithContext(ctx), WithHTTP(&http.Client{Transport: captchaTransport(func(request *http.Request) (*http.Response, error) {
		require.NoError(t, request.Context().Err())
		cancel()
		return nil, request.Context().Err()
	})}))
	require.NoError(t, err)
	response, err := client.Verify(dummyToken, PostOptions{})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, response)
}
