// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/models/asymkey"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	webcontext "forgejo.org/services/context"
	runtime "forgejo.org/services/extensions"

	"code.forgejo.org/go-chi/session"
)

const maxCallbackRequestBytes = 64 << 10

type callbackAdmission struct {
	instanceID   string
	actorID      int64
	sessionID    string
	session      session.Store
	authority    extension.Authority
	capabilities map[string]struct{}
	requestCtx   context.Context
	cancel       context.CancelFunc
}

type admissionRegistry struct {
	mu         sync.Mutex
	entries    map[string]*callbackAdmission
	byInstance map[string]map[string]struct{}
}

var nativeAdmissions = admissionRegistry{entries: make(map[string]*callbackAdmission), byInstance: make(map[string]map[string]struct{})}

func (registry *admissionRegistry) issue(entry *callbackAdmission) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	registry.mu.Lock()
	if registry.entries == nil {
		registry.entries = make(map[string]*callbackAdmission)
	}
	if registry.byInstance == nil {
		registry.byInstance = make(map[string]map[string]struct{})
	}
	registry.entries[token] = entry
	if registry.byInstance[entry.instanceID] == nil {
		registry.byInstance[entry.instanceID] = make(map[string]struct{})
	}
	registry.byInstance[entry.instanceID][token] = struct{}{}
	registry.mu.Unlock()
	return token, nil
}

func (registry *admissionRegistry) get(token string) *callbackAdmission {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.entries[token]
}

func (registry *admissionRegistry) revoke(token string) {
	registry.mu.Lock()
	entry := registry.entries[token]
	delete(registry.entries, token)
	if entry != nil {
		delete(registry.byInstance[entry.instanceID], token)
		if len(registry.byInstance[entry.instanceID]) == 0 {
			delete(registry.byInstance, entry.instanceID)
		}
	}
	registry.mu.Unlock()
	if entry != nil && entry.cancel != nil {
		entry.cancel()
	}
}

func (registry *admissionRegistry) revokeInstance(instanceID string) {
	registry.mu.Lock()
	tokens := registry.byInstance[instanceID]
	entries := make([]*callbackAdmission, 0, len(tokens))
	for token := range tokens {
		if entry := registry.entries[token]; entry != nil {
			entries = append(entries, entry)
			delete(registry.entries, token)
		}
	}
	delete(registry.byInstance, instanceID)
	registry.mu.Unlock()
	for _, entry := range entries {
		if entry.cancel != nil {
			entry.cancel()
		}
	}
}

// RevokeAdmissionsForSession detaches active extension requests at native logout.
func RevokeAdmissionsForSession(sessionID string) {
	nativeAdmissions.mu.Lock()
	var tokens []string
	for token, entry := range nativeAdmissions.entries {
		if entry.sessionID == sessionID {
			tokens = append(tokens, token)
		}
	}
	nativeAdmissions.mu.Unlock()
	for _, token := range tokens {
		nativeAdmissions.revoke(token)
	}
}

// RevokeAdmissionsForInstance cancels all live requests owned by a stopped runtime.
func RevokeAdmissionsForInstance(instanceID string) {
	nativeAdmissions.revokeInstance(instanceID)
}

func sessionGeneration(ctx *webcontext.Context) (string, error) {
	if !nativeSession(ctx) || ctx.Session.ID() == "" || setting.SecretKey == "" {
		return "", errors.New("native session generation unavailable")
	}
	mac := hmac.New(sha256.New, []byte(setting.SecretKey))
	_, _ = mac.Write([]byte("forgejo-native-extension-session\x00"))
	_, _ = mac.Write([]byte(ctx.Session.ID()))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strconv.FormatInt(ctx.Doer.ID, 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func createAdmission(requestCtx context.Context, ctx *webcontext.Context, descriptor runtime.Descriptor, generation string, authority extension.Authority) (string, extension.Authority, error) {
	if requestCtx == nil || requestCtx.Err() != nil || descriptor.InstanceID == "" || authority.ExtensionID != descriptor.Manifest.ID || ctx.Doer == nil || authority.Actor.ID != strconv.FormatInt(ctx.Doer.ID, 10) {
		return "", extension.Authority{}, errors.New("native extension admission unavailable")
	}
	if generation == "" {
		return "", extension.Authority{}, errors.New("native extension admission unavailable")
	}
	authority.InstanceID = descriptor.InstanceID
	authority.SessionGeneration = generation
	capabilities := make(map[string]struct{}, len(descriptor.Manifest.Capabilities))
	for _, capability := range descriptor.Manifest.Capabilities {
		capabilities[capability] = struct{}{}
	}
	admissionCtx, cancel := context.WithCancel(requestCtx)
	entry := &callbackAdmission{
		instanceID:   descriptor.InstanceID,
		actorID:      ctx.Doer.ID,
		sessionID:    ctx.Session.ID(),
		session:      ctx.Session,
		authority:    authority,
		capabilities: capabilities,
		requestCtx:   admissionCtx,
		cancel:       cancel,
	}
	token, err := nativeAdmissions.issue(entry)
	if err != nil {
		cancel()
		return "", extension.Authority{}, errors.New("native extension admission unavailable")
	}
	return token, authority, nil
}

// CallbackHandlerForInstance returns the private native capability endpoint
// bound to one extension process instance.
func CallbackHandlerForInstance(instanceID string) http.Handler {
	return callbackHandler(instanceID, false)
}

// CallbackHandlerForService returns the shared application IPC endpoint. It
// accepts only admissions explicitly declared for the service bridge.
func CallbackHandlerForService() http.Handler {
	return callbackHandler("", true)
}

func callbackHandler(instanceID string, service bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != extension.NativeCallbackPath {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "invalid callback request", http.StatusBadRequest)
			return
		}
		token := r.Header.Get(extension.AdmissionHeader)
		if len(token) != 43 {
			http.Error(w, "native extension admission required", http.StatusUnauthorized)
			return
		}
		entry := nativeAdmissions.get(token)
		if entry == nil || entry.requestCtx.Err() != nil {
			http.Error(w, "native extension admission expired", http.StatusUnauthorized)
			return
		}
		if service {
			if _, declared := entry.capabilities[extension.CapabilityServiceBridge]; !declared {
				http.Error(w, "native service bridge not declared", http.StatusForbidden)
				return
			}
		} else if instanceID == "" || entry.instanceID != instanceID {
			http.Error(w, "native extension admission rejected", http.StatusForbidden)
			return
		}
		request, ok := decodeCallbackRequest(w, r)
		if !ok {
			return
		}
		if !sameAuthority(request.Authority, entry.authority) {
			http.Error(w, "native extension authority rejected", http.StatusForbidden)
			return
		}
		capability, supported := callbackCapability(request.Operation)
		if !supported {
			http.Error(w, "unsupported native capability", http.StatusNotImplemented)
			return
		}
		if _, declared := entry.capabilities[capability]; !declared {
			http.Error(w, "native capability not declared", http.StatusForbidden)
			return
		}
		switch request.Operation {
		case extension.OperationCurrentActor:
			if request.RepositoryID != "" || request.Query != "" || request.Cursor != "" || request.Limit != 0 || request.Organization != "" {
				http.Error(w, "invalid native capability request", http.StatusBadRequest)
				return
			}
		case extension.OperationRepository:
			if mustParsePositiveID(request.RepositoryID) == 0 || request.Query != "" || request.Cursor != "" || request.Limit != 0 || request.Organization != "" {
				http.Error(w, "invalid native capability request", http.StatusBadRequest)
				return
			}
		case extension.OperationOwnedRepositories:
			if request.RepositoryID != "" || request.Organization != "" || request.Limit < 1 || request.Limit > 100 || len(request.Query) > 256 || len(request.Cursor) > 4096 {
				http.Error(w, "invalid native capability request", http.StatusBadRequest)
				return
			}
		case extension.OperationOrganizationOwner:
			if request.RepositoryID != "" || request.Query != "" || request.Cursor != "" || request.Limit != 0 || request.Organization == "" || len(request.Organization) > 255 {
				http.Error(w, "invalid native capability request", http.StatusBadRequest)
				return
			}
		case extension.OperationPublicSSHKeys:
			if request.RepositoryID != "" || request.Query != "" || request.Cursor != "" || request.Limit != 0 || request.Organization != "" {
				http.Error(w, "invalid native capability request", http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, "unsupported native capability", http.StatusNotImplemented)
			return
		}
		callbackCtx, cancel := context.WithTimeout(entry.requestCtx, 5*time.Second)
		stop := context.AfterFunc(r.Context(), cancel)
		defer func() {
			stop()
			cancel()
		}()
		var response extension.CallbackResponse
		var status int
		switch request.Operation {
		case extension.OperationCurrentActor:
			actor, actorStatus := resolveCurrentActor(callbackCtx, entry)
			status = actorStatus
			if status != http.StatusOK {
				http.Error(w, "native actor is unavailable", status)
				return
			}
			response.Actor = &actor
		case extension.OperationRepository:
			response.Repository, status = resolveRepository(callbackCtx, entry, request.RepositoryID)
			if status != http.StatusOK {
				http.Error(w, "native repository is unavailable", status)
				return
			}
		case extension.OperationOwnedRepositories:
			response.Page, status = searchOwnedRepositories(callbackCtx, entry, request)
			if status != http.StatusOK {
				http.Error(w, "native repositories are unavailable", status)
				return
			}
		case extension.OperationOrganizationOwner:
			response.Owner, status = organizationOwner(callbackCtx, entry, request.Organization)
			if status != http.StatusOK {
				http.Error(w, "native organization authority is unavailable", status)
				return
			}
		case extension.OperationPublicSSHKeys:
			response.PublicKeys, status = publicSSHKeys(callbackCtx, entry)
			if status != http.StatusOK {
				http.Error(w, "native public keys are unavailable", status)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(response)
	})
}

type repositorySearchCursor struct {
	Page  int    `json:"page"`
	Limit int    `json:"limit"`
	Query string `json:"query"`
}

func searchOwnedRepositories(ctx context.Context, admission *callbackAdmission, request extension.CallbackRequest) (*extension.RepositoryPage, int) {
	user, status := resolveCurrentUser(ctx, admission)
	if status != http.StatusOK {
		return nil, status
	}
	page := 1
	if request.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(request.Cursor)
		var cursor repositorySearchCursor
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err != nil || decoder.Decode(&cursor) != nil || decoder.Decode(new(any)) != io.EOF || cursor.Page < 2 || cursor.Page > 10000 || cursor.Limit != request.Limit || cursor.Query != request.Query {
			return nil, http.StatusBadRequest
		}
		page = cursor.Page
	}
	options := &repo_model.SearchRepoOptions{
		ListOptions: db.ListOptions{Page: page, PageSize: request.Limit},
		Actor:       user,
		Keyword:     request.Query,
		OwnerID:     user.ID,
		Private:     true,
		Collaborate: optional.Some(false),
		UnitType:    unit.TypeCode,
	}
	repositories, total, err := repo_model.SearchRepositoryByName(ctx, options)
	if ctx.Err() != nil {
		return nil, http.StatusUnauthorized
	}
	if err != nil {
		return nil, http.StatusServiceUnavailable
	}
	pageResult := &extension.RepositoryPage{Items: make([]extension.Repository, 0, min(len(repositories), request.Limit))}
	hasMore := int64(page*request.Limit) < total
	for _, repository := range repositories {
		resolved, resultStatus := resolveRepository(ctx, admission, strconv.FormatInt(repository.ID, 10))
		if resultStatus == http.StatusNotFound {
			continue
		}
		if resultStatus != http.StatusOK {
			return nil, resultStatus
		}
		pageResult.Items = append(pageResult.Items, *resolved)
	}
	if hasMore {
		if page == 10000 {
			return nil, http.StatusBadRequest
		}
		cursor, err := json.Marshal(repositorySearchCursor{Page: page + 1, Limit: request.Limit, Query: request.Query})
		if err != nil {
			return nil, http.StatusServiceUnavailable
		}
		pageResult.NextCursor = base64.RawURLEncoding.EncodeToString(cursor)
	}
	return pageResult, http.StatusOK
}

func organizationOwner(ctx context.Context, admission *callbackAdmission, name string) (*bool, int) {
	user, status := resolveCurrentUser(ctx, admission)
	if status != http.StatusOK {
		return nil, status
	}
	org, err := organization.GetOrgByName(ctx, name)
	if ctx.Err() != nil {
		return nil, http.StatusUnauthorized
	}
	if organization.IsErrOrgNotExist(err) {
		owner := false
		return &owner, http.StatusOK
	}
	if err != nil {
		return nil, http.StatusServiceUnavailable
	}
	ownerValue, err := organization.IsOrganizationOwner(ctx, org.ID, user.ID)
	if ctx.Err() != nil {
		return nil, http.StatusUnauthorized
	}
	if err != nil {
		return nil, http.StatusServiceUnavailable
	}
	return &ownerValue, http.StatusOK
}

func publicSSHKeys(ctx context.Context, admission *callbackAdmission) ([]extension.PublicKey, int) {
	user, status := resolveCurrentUser(ctx, admission)
	if status != http.StatusOK {
		return nil, status
	}
	keys, count, err := db.FindAndCount[asymkey.PublicKey](ctx, asymkey.FindPublicKeyOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: 101},
		OwnerID:     user.ID,
		KeyTypes:    []asymkey.KeyType{asymkey.KeyTypeUser},
	})
	if ctx.Err() != nil {
		return nil, http.StatusUnauthorized
	}
	if err != nil || count > 100 {
		return nil, http.StatusServiceUnavailable
	}
	result := make([]extension.PublicKey, 0, len(keys))
	for _, key := range keys {
		result = append(result, extension.PublicKey{ID: strconv.FormatInt(key.ID, 10), Key: key.Content})
	}
	return result, http.StatusOK
}

func sameAuthority(actual, expected extension.Authority) bool {
	if actual.ExtensionID != expected.ExtensionID || actual.InstanceID != expected.InstanceID ||
		actual.SessionGeneration != expected.SessionGeneration || actual.Contribution != expected.Contribution ||
		actual.Actor != expected.Actor {
		return false
	}
	if actual.Repository == nil || expected.Repository == nil {
		return actual.Repository == nil && expected.Repository == nil
	}
	return *actual.Repository == *expected.Repository
}

func callbackCapability(operation string) (string, bool) {
	switch operation {
	case extension.OperationCurrentActor:
		return extension.CapabilityActorRead, true
	case extension.OperationRepository:
		return extension.CapabilityRepositoryRead, true
	case extension.OperationOwnedRepositories:
		return extension.CapabilityOwnedRepositoriesSearch, true
	case extension.OperationOrganizationOwner:
		return extension.CapabilityOrganizationOwnership, true
	case extension.OperationPublicSSHKeys:
		return extension.CapabilityPublicKeysRead, true
	default:
		return "", false
	}
}

func decodeCallbackRequest(w http.ResponseWriter, r *http.Request) (extension.CallbackRequest, bool) {
	if r.ContentLength > maxCallbackRequestBytes {
		http.Error(w, "callback request exceeds limits", http.StatusRequestEntityTooLarge)
		return extension.CallbackRequest{}, false
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackRequestBytes+1))
	if err != nil || len(data) > maxCallbackRequestBytes {
		http.Error(w, "invalid callback request", http.StatusBadRequest)
		return extension.CallbackRequest{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request extension.CallbackRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid callback request", http.StatusBadRequest)
		return extension.CallbackRequest{}, false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid callback request", http.StatusBadRequest)
		return extension.CallbackRequest{}, false
	}
	return request, true
}

func resolveCurrentActor(ctx context.Context, admission *callbackAdmission) (extension.Actor, int) {
	user, status := resolveCurrentUser(ctx, admission)
	if status != http.StatusOK {
		return extension.Actor{}, status
	}
	return extension.Actor{ID: strconv.FormatInt(user.ID, 10), Username: user.Name, SiteAdmin: user.IsAdmin}, http.StatusOK
}

func resolveRepository(ctx context.Context, admission *callbackAdmission, repositoryID string) (*extension.Repository, int) {
	user, status := resolveCurrentUser(ctx, admission)
	if status != http.StatusOK {
		return nil, status
	}
	id := mustParsePositiveID(repositoryID)
	if id == 0 {
		return nil, http.StatusBadRequest
	}
	repository, err := repo_model.GetRepositoryByID(ctx, id)
	if ctx.Err() != nil {
		return nil, http.StatusUnauthorized
	}
	if repo_model.IsErrRepoNotExist(err) {
		return nil, http.StatusNotFound
	}
	if err != nil {
		return nil, http.StatusServiceUnavailable
	}
	permission, err := access_model.GetUserRepoPermission(ctx, repository, user)
	if ctx.Err() != nil {
		return nil, http.StatusUnauthorized
	}
	if err != nil {
		return nil, http.StatusServiceUnavailable
	}
	if !permission.CanRead(unit.TypeCode) {
		return nil, http.StatusNotFound
	}
	if err := repository.LoadOwner(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, http.StatusUnauthorized
		}
		return nil, http.StatusServiceUnavailable
	}
	level := "read"
	if permission.IsAdmin() {
		level = "admin"
	} else if permission.CanWrite(unit.TypeCode) {
		level = "write"
	}
	return &extension.Repository{ID: strconv.FormatInt(repository.ID, 10), Owner: repository.Owner.Name, Name: repository.Name, Permission: level}, http.StatusOK
}

func mustParsePositiveID(value string) int64 {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != value {
		return 0
	}
	return id
}

func resolveCurrentUser(ctx context.Context, admission *callbackAdmission) (*user_model.User, int) {
	if err := ctx.Err(); err != nil {
		return nil, http.StatusUnauthorized
	}
	store, err := admission.session.Read(admission.sessionID)
	if err != nil || store == nil {
		return nil, http.StatusUnauthorized
	}
	uid, ok := store.Get("uid").(int64)
	if !ok || uid != admission.actorID {
		return nil, http.StatusUnauthorized
	}
	user, err := user_model.GetUserByID(ctx, admission.actorID)
	if ctx.Err() != nil {
		return nil, http.StatusUnauthorized
	}
	if err != nil {
		return nil, http.StatusServiceUnavailable
	}
	if !user.IsAccessAllowed(ctx) {
		return nil, http.StatusUnauthorized
	}
	if ctx.Err() != nil {
		return nil, http.StatusUnauthorized
	}
	return user, http.StatusOK
}
