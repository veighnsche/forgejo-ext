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
	"os"
	"strconv"
	"strings"
)

const NativeCallbackPath = "/v1/native"

const maxCallbackResponseBytes = 1 << 20

// ErrRepositoryNotVisible is returned only when the host's repository callback
// confirms that a repository is missing or not readable by the admitted actor.
var ErrRepositoryNotVisible = errors.New("repository is not visible")

const (
	OperationCurrentActor      = "actor.current"
	OperationRepository        = "repository.get"
	OperationOwnedRepositories = "repositories.owned.search"
	OperationOrganizationOwner = "organization.owned"
	OperationPublicSSHKeys     = "user.public_keys"
)

// CallbackRequest is sent only on the private host socket.
// The host resolves the actor from the admission, never from this body.
type CallbackRequest struct {
	Operation    string    `json:"operation"`
	RepositoryID string    `json:"repository_id,omitempty"`
	Query        string    `json:"query,omitempty"`
	Cursor       string    `json:"cursor,omitempty"`
	Limit        int       `json:"limit,omitempty"`
	Organization string    `json:"organization,omitempty"`
	Authority    Authority `json:"authority"`
}

type CallbackResponse struct {
	Actor      *Actor          `json:"actor,omitempty"`
	Repository *Repository     `json:"repository,omitempty"`
	Page       *RepositoryPage `json:"page,omitempty"`
	Owner      *bool           `json:"owner,omitempty"`
	PublicKeys []PublicKey     `json:"public_keys"`
	ErrorCode  string          `json:"error_code,omitempty"`
}

// RequestContext accepts authority only from the private extension listener.
// The admission is read separately so encoding Authority cannot expose it.
func RequestContext(r *http.Request) (Authority, error) {
	var authority Authority
	value := r.Header.Get(ContextHeader)
	if value == "" || len(value) > 8192 {
		return authority, errors.New("missing or oversized extension authority")
	}
	dec := json.NewDecoder(strings.NewReader(value))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&authority); err != nil {
		return Authority{}, fmt.Errorf("decode extension authority: %w", err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Authority{}, errors.New("extension authority must contain one object")
	}
	if authority.ExtensionID == "" || authority.InstanceID == "" || authority.SessionGeneration == "" ||
		authority.Contribution.ID == "" || authority.Contribution.Kind == "" || authority.Contribution.Scope == "" ||
		authority.Contribution.Action == "" || !decimalID(authority.Actor.ID) {
		return Authority{}, errors.New("incomplete extension authority")
	}
	if authority.Repository != nil && !decimalID(authority.Repository.ID) {
		return Authority{}, errors.New("invalid repository id")
	}
	authority.admission = r.Header.Get(AdmissionHeader)
	if authority.admission == "" {
		return Authority{}, errors.New("missing extension admission")
	}
	authority.callbackSocket = os.Getenv(CallbackEnv)
	if serviceSocket := os.Getenv(ServiceCallbackEnv); serviceSocket != "" {
		authority.callbackSocket = serviceSocket
		authority.serviceCallback = true
		if raw := os.Getenv(ServiceCallbackPeerEnv); raw != "" {
			uid, err := strconv.ParseUint(raw, 10, 32)
			if err != nil {
				return Authority{}, errors.New("invalid service callback peer identity")
			}
			authority.servicePeerUID = uint32(uid)
			authority.servicePeerPinned = true
		}
	}
	return authority, nil
}

func decimalID(id string) bool {
	if id == "" || id[0] == '0' {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (a Authority) Native() NativeClient {
	// Per-instance hosts spawn the extension without changing users, so that
	// callback listener runs as this process's own UID. The shared service
	// callback instead runs inside Forgejo under a fixed service identity,
	// pinned by FORGEJO_EXTENSION_SERVICE_CALLBACK_PEER_UID. The dial
	// verifies the listener against that identity before sending anything.
	expected := uint32(os.Geteuid())
	if a.serviceCallback && a.servicePeerPinned {
		expected = a.servicePeerUID
	}
	return &nativeClient{socket: a.callbackSocket, admission: a.admission, authority: a, expectedUID: expected}
}

type nativeClient struct {
	socket      string
	admission   string
	authority   Authority
	expectedUID uint32
}

func (c *nativeClient) call(ctx context.Context, request CallbackRequest) (CallbackResponse, error) {
	if c.socket == "" || c.admission == "" {
		return CallbackResponse{}, errors.New("native callback is unavailable")
	}
	request.Authority = c.authority
	data, err := json.Marshal(request)
	if err != nil {
		return CallbackResponse{}, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.socket)
		if err != nil {
			return nil, err
		}
		peer, err := PeerCredential(conn)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if peer.UID != c.expectedUID {
			_ = conn.Close()
			return nil, errors.New("native callback host peer is not permitted")
		}
		return conn, nil
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://extension-host"+NativeCallbackPath, bytes.NewReader(data))
	if err != nil {
		return CallbackResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(AdmissionHeader, c.admission)
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return CallbackResponse{}, ctx.Err()
		}
		return CallbackResponse{}, errors.New("native callback request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		// The callback uses 404 for both a missing repository and one the actor
		// cannot read. A 403 instead means the extension's callback admission or
		// capability was rejected, so keep it an ordinary authority error.
		if request.Operation == OperationRepository && response.StatusCode == http.StatusNotFound {
			return CallbackResponse{}, ErrRepositoryNotVisible
		}
		return CallbackResponse{}, fmt.Errorf("native callback returned HTTP %d", response.StatusCode)
	}
	return decodeCallbackResponse(response.Body, request)
}

func decodeCallbackResponse(body io.Reader, request CallbackRequest) (CallbackResponse, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxCallbackResponseBytes+1))
	if err != nil {
		return CallbackResponse{}, errors.New("read native callback response failed")
	}
	if len(data) > maxCallbackResponseBytes {
		return CallbackResponse{}, errors.New("native callback response exceeds limit")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return CallbackResponse{}, errors.New("native callback response must be an object")
	}
	var result CallbackResponse
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return CallbackResponse{}, errors.New("invalid native callback response")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return CallbackResponse{}, errors.New("native callback response must contain one object")
	}
	if result.ErrorCode != "" {
		return CallbackResponse{}, errors.New("native callback rejected request")
	}
	if !validCallbackResult(result, request) {
		return CallbackResponse{}, errors.New("native callback returned an invalid result")
	}
	return result, nil
}

func validCallbackResult(result CallbackResponse, request CallbackRequest) bool {
	fields := 0
	for _, present := range []bool{result.Actor != nil, result.Repository != nil, result.Page != nil, result.Owner != nil, result.PublicKeys != nil} {
		if present {
			fields++
		}
	}
	if fields != 1 {
		return false
	}
	switch request.Operation {
	case OperationCurrentActor:
		return result.Actor != nil && decimalID(result.Actor.ID)
	case OperationRepository:
		if result.Repository == nil || !decimalID(result.Repository.ID) || result.Repository.ID != request.RepositoryID || result.Repository.Owner == "" || result.Repository.Name == "" {
			return false
		}
		switch result.Repository.Permission {
		case "read", "write", "admin":
			return true
		default:
			return false
		}
	case OperationOwnedRepositories:
		if result.Page == nil || result.Page.Items == nil || len(result.Page.Items) > request.Limit {
			return false
		}
		for _, repository := range result.Page.Items {
			if !decimalID(repository.ID) {
				return false
			}
		}
		return true
	case OperationOrganizationOwner:
		return result.Owner != nil
	case OperationPublicSSHKeys:
		if result.PublicKeys == nil {
			return false
		}
		for _, key := range result.PublicKeys {
			if !decimalID(key.ID) || key.Key == "" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (c *nativeClient) CurrentActor(ctx context.Context) (Actor, error) {
	result, err := c.call(ctx, CallbackRequest{Operation: OperationCurrentActor})
	if err != nil {
		return Actor{}, err
	}
	return *result.Actor, nil
}

func (c *nativeClient) Repository(ctx context.Context, id string) (Repository, error) {
	if !decimalID(id) {
		return Repository{}, errors.New("invalid repository id")
	}
	result, err := c.call(ctx, CallbackRequest{Operation: OperationRepository, RepositoryID: id})
	if err != nil {
		return Repository{}, err
	}
	return *result.Repository, nil
}

func (c *nativeClient) SearchOwnedRepositories(ctx context.Context, query, cursor string, limit int) (RepositoryPage, error) {
	if limit < 1 || limit > 100 || len(query) > 256 || len(cursor) > 4096 {
		return RepositoryPage{}, errors.New("invalid repository search bounds")
	}
	result, err := c.call(ctx, CallbackRequest{Operation: OperationOwnedRepositories, Query: query, Cursor: cursor, Limit: limit})
	if err != nil {
		return RepositoryPage{}, err
	}
	return *result.Page, nil
}

func (c *nativeClient) OrganizationOwner(ctx context.Context, organization string) (bool, error) {
	if organization == "" || len(organization) > 255 {
		return false, errors.New("invalid organization")
	}
	result, err := c.call(ctx, CallbackRequest{Operation: OperationOrganizationOwner, Organization: organization})
	if err != nil {
		return false, err
	}
	return *result.Owner, nil
}

func (c *nativeClient) PublicSSHKeys(ctx context.Context) ([]PublicKey, error) {
	result, err := c.call(ctx, CallbackRequest{Operation: OperationPublicSSHKeys})
	if err != nil {
		return nil, err
	}
	return result.PublicKeys, nil
}
