// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	extension "forgejo.org/extension-sdk"

	"code.forgejo.org/go-chi/session"
	"github.com/stretchr/testify/require"
)

type callbackTestSession struct {
	session.Store
	uid any
}

func (s callbackTestSession) Read(string) (session.RawStore, error) {
	return callbackTestRawStore{uid: s.uid}, nil
}

type callbackTestRawStore struct {
	session.RawStore
	uid any
}

func (s callbackTestRawStore) Get(key any) any {
	if key == "uid" {
		return s.uid
	}
	return nil
}

func TestNativeCallbackRequiresInstanceCapabilityAndLiveSession(t *testing.T) {
	requestCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	expectedAuthority := extension.Authority{
		ExtensionID:       "soda",
		InstanceID:        "instance-a",
		SessionGeneration: "session-generation",
		Contribution:      extension.Contribution{ID: "spaces", Kind: "page", Scope: "global", Action: "get"},
		Actor:             extension.Actor{ID: "42", Username: "soda-tester"},
	}
	entry := &callbackAdmission{
		instanceID:   "instance-a",
		actorID:      42,
		sessionID:    "private-session-id",
		session:      callbackTestSession{uid: int64(43)},
		authority:    expectedAuthority,
		capabilities: map[string]struct{}{extension.CapabilityActorRead: {}},
		requestCtx:   requestCtx,
	}
	token, err := nativeAdmissions.issue(entry)
	require.NoError(t, err)
	t.Cleanup(func() { nativeAdmissions.revoke(token) })

	makeRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, extension.NativeCallbackPath, strings.NewReader(forgedRequestBody()))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(extension.AdmissionHeader, token)
		return req
	}

	wrongInstance := httptest.NewRecorder()
	CallbackHandlerForInstance("instance-b").ServeHTTP(wrongInstance, makeRequest())
	require.Equal(t, http.StatusForbidden, wrongInstance.Code)

	undeclared := &callbackAdmission{instanceID: "instance-a", authority: expectedAuthority, capabilities: map[string]struct{}{}, requestCtx: requestCtx}
	undeclaredToken, err := nativeAdmissions.issue(undeclared)
	require.NoError(t, err)
	t.Cleanup(func() { nativeAdmissions.revoke(undeclaredToken) })
	undeclaredResponse := httptest.NewRecorder()
	undeclaredRequest := makeRequest()
	undeclaredRequest.Header.Set(extension.AdmissionHeader, undeclaredToken)
	CallbackHandlerForInstance("instance-a").ServeHTTP(undeclaredResponse, undeclaredRequest)
	require.Equal(t, http.StatusForbidden, undeclaredResponse.Code)

	forgedContext := httptest.NewRecorder()
	forgedRequest := httptest.NewRequest(http.MethodPost, extension.NativeCallbackPath, strings.NewReader(strings.Replace(forgedRequestBody(), `"action":"get"`, `"action":"patch"`, 1)))
	forgedRequest.Header.Set("Content-Type", "application/json")
	forgedRequest.Header.Set(extension.AdmissionHeader, token)
	CallbackHandlerForInstance("instance-a").ServeHTTP(forgedContext, forgedRequest)
	require.Equal(t, http.StatusForbidden, forgedContext.Code)

	staleGeneration := httptest.NewRecorder()
	staleRequest := httptest.NewRequest(http.MethodPost, extension.NativeCallbackPath, strings.NewReader(strings.Replace(forgedRequestBody(), `"session-generation"`, `"old-generation"`, 1)))
	staleRequest.Header.Set("Content-Type", "application/json")
	staleRequest.Header.Set(extension.AdmissionHeader, token)
	CallbackHandlerForInstance("instance-a").ServeHTTP(staleGeneration, staleRequest)
	require.Equal(t, http.StatusForbidden, staleGeneration.Code)

	staleSession := httptest.NewRecorder()
	CallbackHandlerForInstance("instance-a").ServeHTTP(staleSession, makeRequest())
	require.Equal(t, http.StatusUnauthorized, staleSession.Code)

	nativeAdmissions.revoke(token)
	revoked := httptest.NewRecorder()
	CallbackHandlerForInstance("instance-a").ServeHTTP(revoked, makeRequest())
	require.Equal(t, http.StatusUnauthorized, revoked.Code)

	cancel()
	expired := httptest.NewRecorder()
	CallbackHandlerForInstance("instance-a").ServeHTTP(expired, makeRequest())
	require.Equal(t, http.StatusUnauthorized, expired.Code)
}

func forgedRequestBody() string {
	return `{"operation":"actor.current","authority":{"extension_id":"soda","instance_id":"instance-a","session_generation":"session-generation","contribution":{"id":"spaces","kind":"page","scope":"global","action":"get"},"actor":{"id":"42","username":"soda-tester","site_admin":false}}}`
}

func TestNativeCallbackRejectsMalformedAndOversizedRequests(t *testing.T) {
	for name, body := range map[string]string{
		"multiple values": `{"operation":"actor.current"} {}`,
		"unknown field":   `{"operation":"actor.current","actor_id":"42"}`,
		"oversized":       `{"operation":"actor.current"}` + strings.Repeat(" ", maxCallbackRequestBytes),
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, extension.NativeCallbackPath, strings.NewReader(body))
			_, ok := decodeCallbackRequest(response, request)
			require.False(t, ok)
			require.True(t, response.Code == http.StatusBadRequest || response.Code == http.StatusRequestEntityTooLarge)
		})
	}
}

func TestStoppedInstanceRevokesItsAdmissions(t *testing.T) {
	firstContext, cancelFirst := context.WithCancel(context.Background())
	secondContext, cancelSecond := context.WithCancel(context.Background())
	t.Cleanup(cancelFirst)
	t.Cleanup(cancelSecond)
	firstRequestContext, cancelFirstRequest := context.WithCancel(firstContext)
	secondRequestContext, cancelSecondRequest := context.WithCancel(secondContext)
	t.Cleanup(cancelFirstRequest)
	t.Cleanup(cancelSecondRequest)
	firstToken, err := nativeAdmissions.issue(&callbackAdmission{instanceID: "stopped-instance", requestCtx: firstRequestContext, cancel: cancelFirstRequest})
	require.NoError(t, err)
	secondToken, err := nativeAdmissions.issue(&callbackAdmission{instanceID: "live-instance", requestCtx: secondRequestContext, cancel: cancelSecondRequest})
	require.NoError(t, err)
	t.Cleanup(func() { nativeAdmissions.revoke(firstToken) })
	t.Cleanup(func() { nativeAdmissions.revoke(secondToken) })

	RevokeAdmissionsForInstance("stopped-instance")
	require.ErrorIs(t, firstRequestContext.Err(), context.Canceled)
	require.NoError(t, secondRequestContext.Err())
	require.Nil(t, nativeAdmissions.get(firstToken))
	require.NotNil(t, nativeAdmissions.get(secondToken))
}

func TestServiceCallbackRequiresExplicitBridgeCapability(t *testing.T) {
	requestCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	entry := &callbackAdmission{
		instanceID:   "instance-a",
		actorID:      42,
		sessionID:    "private-session-id",
		session:      callbackTestSession{uid: int64(42)},
		authority:    extension.Authority{ExtensionID: "soda", InstanceID: "instance-a", SessionGeneration: "session-generation", Contribution: extension.Contribution{ID: "spaces", Kind: "page", Scope: "global", Action: "get"}, Actor: extension.Actor{ID: "42", Username: "soda-tester"}},
		capabilities: map[string]struct{}{extension.CapabilityActorRead: {}},
		requestCtx:   requestCtx,
	}
	token, err := nativeAdmissions.issue(entry)
	require.NoError(t, err)
	t.Cleanup(func() { nativeAdmissions.revoke(token) })
	request := httptest.NewRequest(http.MethodPost, extension.NativeCallbackPath, strings.NewReader(forgedRequestBody()))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(extension.AdmissionHeader, token)
	response := httptest.NewRecorder()
	CallbackHandlerForService().ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
}
