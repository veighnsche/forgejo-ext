// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	extension "forgejo.org/extension-sdk"
	runtime "forgejo.org/services/extensions"

	"github.com/stretchr/testify/require"
)

func TestContributionAuthorizationIsOptionalAndNarrowing(t *testing.T) {
	page := extension.Page{ID: "spaces", Scope: "repository"}
	contribution := pageContribution(page, http.MethodPatch)
	called := false
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		called = true
		require.Equal(t, "http://extension"+contributionAuthorizationPath, request.URL.String())
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		deadline, ok := request.Context().Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(contributionAuthorizationTTL), deadline, 100*time.Millisecond)
		var input extension.ContributionRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&input))
		require.Equal(t, extension.ContributionRequest{
			ActorID:      "42",
			Contribution: extension.Contribution{ID: "spaces", Kind: "page", Scope: "repository", Action: "patch"},
			RepositoryID: "7",
		}, input)
		return authorizationResponse(http.StatusOK, "application/json", `{"allowed":true}`), nil
	})
	descriptor := runtime.Descriptor{Manifest: extension.Manifest{Capabilities: []string{extension.CapabilityContributionAuthorize}}}
	require.True(t, authorizesContribution(context.Background(), descriptor, transport, contribution, "7", 42))
	require.True(t, called)

	called = false
	require.True(t, authorizesContribution(context.Background(), runtime.Descriptor{}, nil, contribution, "7", 42))
	require.False(t, called, "an extension without the capability uses only core permission checks")

	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return authorizationResponse(http.StatusOK, "application/json", `{"allowed":false,"denial_code":"policy"}`), nil
	})
	require.False(t, authorizesContribution(context.Background(), descriptor, transport, contribution, "7", 42))
}

func TestContributionAuthorizationFailsClosed(t *testing.T) {
	descriptor := runtime.Descriptor{Manifest: extension.Manifest{Capabilities: []string{extension.CapabilityContributionAuthorize}}}
	contribution := pageContribution(extension.Page{ID: "spaces", Scope: "global"}, http.MethodGet)
	for name, roundTrip := range map[string]roundTripFunc{
		"unavailable": func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") },
		"wrong status": func(*http.Request) (*http.Response, error) {
			return authorizationResponse(http.StatusServiceUnavailable, "application/json", `{"allowed":true}`), nil
		},
		"wrong content type": func(*http.Request) (*http.Response, error) {
			return authorizationResponse(http.StatusOK, "text/plain", `{"allowed":true}`), nil
		},
		"malformed": func(*http.Request) (*http.Response, error) {
			return authorizationResponse(http.StatusOK, "application/json", `{"allowed":"yes"}`), nil
		},
		"unknown field": func(*http.Request) (*http.Response, error) {
			return authorizationResponse(http.StatusOK, "application/json", `{"allowed":true,"actor_id":"42"}`), nil
		},
		"multiple values": func(*http.Request) (*http.Response, error) {
			return authorizationResponse(http.StatusOK, "application/json", `{"allowed":true} {}`), nil
		},
		"oversized": func(*http.Request) (*http.Response, error) {
			return authorizationResponse(http.StatusOK, "application/json", `{"allowed":true,"denial_code":"`+strings.Repeat("x", maxContributionResponseBytes)+`"}`), nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, authorizesContribution(context.Background(), descriptor, roundTrip, contribution, "", 42))
		})
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineAware := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})
	require.False(t, authorizesContribution(canceled, descriptor, deadlineAware, contribution, "", 42))
	require.False(t, authorizesContribution(context.Background(), descriptor, deadlineAware, contribution, "", 0))
}

func authorizationResponse(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
