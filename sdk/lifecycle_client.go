// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Lifecycle authorize callback paths. The manager serves the authorize
// path on its existing per-instance/service callback multiplexer; the
// required controller forwards through it before handing a bounded command
// to its host.
const (
	// LifecyclePath is the SDK-owned controller endpoint served by
	// Application.handler before ordinary routes.
	LifecyclePath = "/v1/extensions/lifecycle"
	// LifecycleAuthorizePath is the manager authorize callback path
	// used by LifecycleAuthority.Verify. It performs no requested
	// lifecycle effect.
	LifecycleAuthorizePath = "/v1/extensions/lifecycle/authorize"
)

// Verify invokes the manager's authorize callback for the exact call being
// verified and returns the echoed verification. The request carries the
// opaque manager-issued token in the lifecycle admission header with the
// exact call as its body; the token is never serialized into a record,
// response, log or error. Transport reuses the existing callback socket
// selection with server-peer pinning, and both directions are bounded at
// 16 KiB.
//
// The verifier (manager side) checks token variant, deadline, live
// controller instance, controller capability, catalog target-controller
// relation, target UUID/generation, current selected artifact pin,
// pending/desired operation and complete body equality. Verify enforces its
// half: HTTP 200 with a strictly decoded verification whose echoed call
// equals the presented call byte-for-byte in every field. A mismatched
// echo, malformed body, timeout, non-200 or oversized response fails.
// Error mapping follows the callback contract: 400 malformed, 403
// unauthorized/expired/revoked/mismatched, 503 unavailable.
//
// Verify cannot authorize an operation it was not issued for: the issued
// admission binds the full tuple including the operation, so presenting a
// cleanup (deactivate) admission with an activate call fails body equality.
// It cannot survive command completion either: the manager revokes the
// token after its call, deadline, controller instance stop/replacement or
// superseding intent, and a revoked token answers 403.
func (a LifecycleAuthority) Verify(ctx context.Context, call LifecycleCall) (LifecycleVerification, error) {
	if a.admission == "" || a.callbackSocket == "" {
		return LifecycleVerification{}, errors.New("lifecycle authority is not admitted")
	}
	body, err := json.Marshal(call)
	if err != nil {
		return LifecycleVerification{}, err
	}
	socket := a.callbackSocket
	expected := a.lifecycleExpectedUID()
	transport := &http.Transport{DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
		return dialPinnedUnixSocket(dialCtx, socket, expected)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://extension-host"+LifecycleAuthorizePath, bytes.NewReader(body))
	if err != nil {
		return LifecycleVerification{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(LifecycleAdmissionHeader, a.admission)
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return LifecycleVerification{}, ctx.Err()
		}
		return LifecycleVerification{}, errors.New("lifecycle verification request failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, MaximumLifecycleBodyBytes+1))
	if err != nil {
		return LifecycleVerification{}, errors.New("read lifecycle verification failed")
	}
	if len(data) > MaximumLifecycleBodyBytes {
		return LifecycleVerification{}, errors.New("lifecycle verification exceeds limit")
	}
	switch response.StatusCode {
	case http.StatusOK:
		verification, err := DecodeLifecycleVerification(data)
		if err != nil {
			return LifecycleVerification{}, err
		}
		if verification.Call != call {
			return LifecycleVerification{}, errors.New("lifecycle verification does not echo its call")
		}
		return verification, nil
	case http.StatusBadRequest:
		return LifecycleVerification{}, errors.New("lifecycle verification rejected the call as malformed")
	case http.StatusForbidden:
		return LifecycleVerification{}, errors.New("lifecycle verification is unauthorized, expired, revoked or mismatched")
	case http.StatusServiceUnavailable:
		return LifecycleVerification{}, errors.New("lifecycle verification is unavailable")
	default:
		return LifecycleVerification{}, fmt.Errorf("lifecycle verification returned HTTP %d", response.StatusCode)
	}
}

// ForwardUnix forwards one exact typed call with its opaque authority to the
// dedicated Go private endpoint over a pinned Unix transport. The controller
// owns the socket, peer UID and path constants from trusted local service
// configuration, never request parameters: socket must be an absolute Unix
// socket path and path a local absolute path without query, fragment or URL
// authority. The SDK stays product-neutral (no Soda/Tailnet names or
// routes): it keeps admission unexported, marshals ONLY the exact typed
// LifecycleCall and adds LifecycleAdmissionHeader internally. There is no
// token getter, serialized authority, Actor path, cookie forwarding or
// generic body/proxy method.
//
// The call must be structurally valid (strict decode, known operation) with
// a nonzero deadline; the request context is bounded by the lesser of the
// caller context and the call deadline. Transport dials only the given
// socket with SO_PEERCRED peer-UID pinning and no redirects; both directions
// are bounded at 16 KiB. HTTP 200 must carry a strictly decoded observation
// that fully validates against the presented call (echo, state, grant,
// effect-generation semantics); malformed, oversize, peer, timeout,
// non-200, redirect or semantically invalid responses fail closed without
// logging token, body or path secrets.
//
// ForwardUnix transports authority; the required controller must still Verify
// before forwarding, and the receiving Go service independently invokes its
// authorize callback before restricted effects. It cannot change
// desired/pending/generation or invent cleanup authority.
func (a LifecycleAuthority) ForwardUnix(ctx context.Context, socket string, peerUID uint32, path string, call LifecycleCall) (LifecycleObservation, error) {
	fail := func() (LifecycleObservation, error) {
		return LifecycleObservation{}, errors.New("lifecycle forward is invalid")
	}
	if a.admission == "" {
		return fail()
	}
	if socket == "" || !strings.HasPrefix(socket, "/") || strings.ContainsAny(socket, "?#") || strings.Contains(socket, "://") {
		return fail()
	}
	if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") || strings.Contains(path, "://") {
		return fail()
	}
	if err := ValidateLifecycleCall(call, time.Now().Unix()); err != nil {
		return fail()
	}
	deadline := time.Unix(call.DeadlineUnix, 0)
	forwardCtx := ctx
	if callerDeadline, ok := ctx.Deadline(); !ok || deadline.Before(callerDeadline) {
		var cancel context.CancelFunc
		forwardCtx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	body, err := json.Marshal(call)
	if err != nil {
		return fail()
	}
	if len(body) > MaximumLifecycleBodyBytes {
		return LifecycleObservation{}, errors.New("lifecycle forward exceeds limit")
	}
	transport := &http.Transport{DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
		return dialPinnedUnixSocket(dialCtx, socket, peerUID)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("lifecycle forward does not follow redirects")
		},
	}
	request, err := http.NewRequestWithContext(forwardCtx, http.MethodPost, "http://unix-host"+path, bytes.NewReader(body))
	if err != nil {
		return fail()
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(LifecycleAdmissionHeader, a.admission)
	response, err := client.Do(request)
	if err != nil {
		if forwardCtx.Err() != nil {
			return LifecycleObservation{}, forwardCtx.Err()
		}
		return LifecycleObservation{}, errors.New("lifecycle forward request failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, MaximumLifecycleBodyBytes+1))
	if err != nil {
		return LifecycleObservation{}, errors.New("read lifecycle forward failed")
	}
	if len(data) > MaximumLifecycleBodyBytes {
		return LifecycleObservation{}, errors.New("lifecycle forward exceeds limit")
	}
	if response.StatusCode != http.StatusOK {
		return LifecycleObservation{}, errors.New("lifecycle forward was not accepted")
	}
	observation, err := DecodeLifecycleObservation(data)
	if err != nil {
		return LifecycleObservation{}, err
	}
	if err := ValidateLifecycleObservation(observation, call, time.Now().Unix()); err != nil {
		return LifecycleObservation{}, err
	}
	return observation, nil
}
