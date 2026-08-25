// Copyright 2024, 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"forgejo.org/models/forgefed"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/validation"

	"github.com/google/uuid"
)

func Init() error {
	if !setting.Federation.Enabled {
		return nil
	}
	return initDeliveryQueue()
}

// ErrFederationHostNotAllowed is returned when a host is not allowed by
// the federation host policy (ALLOWED_HOSTS / BLOCKED_HOSTS).
type ErrFederationHostNotAllowed struct {
	Host string
}

func (e ErrFederationHostNotAllowed) Error() string {
	return fmt.Sprintf("federation host %q is not allowed by the host policy", e.Host)
}

func FindOrCreateFederationHost(ctx context.Context, actorURI string) (*forgefed.FederationHost, error) {
	rawActorID, err := fm.NewActorID(actorURI)
	if err != nil {
		return nil, err
	}

	if !setting.FederationHostAllowed(rawActorID.Host) {
		return nil, ErrFederationHostNotAllowed{Host: rawActorID.Host}
	}

	// Operator-blocked hosts are never contacted, even if they pass the
	// allowlist policy.
	blocked, err := forgefed.IsFederationHostBlocked(ctx, rawActorID.Host)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, ErrFederationHostNotAllowed{Host: rawActorID.Host}
	}

	federationHost, err := forgefed.FindFederationHostByFqdnAndPort(ctx, rawActorID.Host, rawActorID.HostPort)
	if err != nil {
		if !forgefed.IsErrFederationHostNotFound(err) {
			return nil, err
		}

		federationHost, err = createFederationHostFromAP(ctx, rawActorID)
	}

	return federationHost, err
}

func FindOrCreateFederatedUser(ctx context.Context, actorURI string) (*user_model.User, *user_model.FederatedUser, *forgefed.FederationHost, error) {
	federationHost, personID, err := findFederationHost(ctx, actorURI)
	if err != nil {
		return nil, nil, nil, err
	}

	user, federatedUser, err := findFederatedUser(ctx, actorURI)
	if err == nil {
		log.Trace("Found local user: %v", user.Name)
		return user, federatedUser, federationHost, nil
	}

	if !user_model.IsErrFederatedUserNotExists(err) {
		return nil, nil, nil, err
	}

	// Fetch the remote user
	apUser, apFederatedUser, err := fetchUserFromAP(ctx, *personID, federationHost)
	if err != nil {
		return nil, nil, nil, err
	}

	// User is an alias, for example in newer Mastodon versions
	// - example.com/@example
	// - example.com/users/example
	// have the ID
	// - example.com/ap/users/<id>
	user, federatedUser, err = findFederatedUser(ctx, apFederatedUser.NormalizedOriginalURL)
	if err == nil {
		log.Trace("Resolved alias %s to %s", actorURI, apFederatedUser.NormalizedOriginalURL)
		return user, federatedUser, federationHost, nil
	}

	err = user_model.CreateFederatedUser(ctx, apUser, apFederatedUser)
	if err != nil {
		return nil, nil, nil, err
	}

	log.Trace("Created user %s with federatedUser %s from distant server", user.LogString(), federatedUser.LogString())
	return apUser, apFederatedUser, federationHost, nil
}

func findFederationHost(ctx context.Context, actorURI string) (*forgefed.FederationHost, *fm.PersonID, error) {
	federationHost, err := FindOrCreateFederationHost(ctx, actorURI)
	if err != nil {
		return nil, nil, err
	}

	actorID, err := fm.NewPersonID(actorURI, string(federationHost.NodeInfo.SoftwareName))
	if err != nil {
		return nil, nil, err
	}

	return federationHost, &actorID, nil
}

func findFederatedUser(ctx context.Context, actorURI string) (*user_model.User, *user_model.FederatedUser, error) {
	federationHost, _, err := findFederationHost(ctx, actorURI)
	if err != nil {
		return nil, nil, err
	}

	actorID, err := fm.NewPersonID(actorURI, string(federationHost.NodeInfo.SoftwareName))
	if err != nil {
		return nil, nil, err
	}

	localUser, federatedUser, err := user_model.FindFederatedUser(ctx, actorID.ID, federationHost.ID)
	if err != nil {
		return nil, nil, err
	}

	return localUser, federatedUser, nil
}

func createFederationHostFromAP(ctx context.Context, actorID fm.ActorID) (*forgefed.FederationHost, error) {
	actionsUser := user_model.NewAPServerActor()

	clientFactory, err := activitypub.GetClientFactory(ctx)
	if err != nil {
		return nil, err
	}

	uri, err := url.Parse(actorID.AsWellKnownNodeInfoURI())
	if err != nil {
		return nil, fmt.Errorf("invalid actor URI: %w", err)
	}

	client, err := clientFactory.WithKeys(ctx, actionsUser, actionsUser.KeyID(), []*url.URL{uri})
	if err != nil {
		return nil, err
	}

	body, err := client.GetBody(actorID.AsWellKnownNodeInfoURI())
	if err != nil {
		return nil, err
	}

	nodeInfoWellKnown, err := forgefed.NewNodeInfoWellKnown(body)
	if err != nil {
		return nil, err
	}

	body, err = client.GetBody(nodeInfoWellKnown.Href)
	if err != nil {
		return nil, err
	}

	nodeInfo, err := forgefed.NewNodeInfo(body)
	if err != nil {
		return nil, err
	}

	// TODO: we should get key material here also to have it immediately
	result, err := forgefed.NewFederationHost(actorID.Host, nodeInfo, actorID.HostPort, actorID.HostSchema)
	if err != nil {
		return nil, err
	}

	err = forgefed.CreateFederationHost(ctx, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func fetchUserFromAP(ctx context.Context, personID fm.PersonID, federationHost *forgefed.FederationHost) (*user_model.User, *user_model.FederatedUser, error) {
	actionsUser := user_model.NewAPServerActor()
	clientFactory, err := activitypub.GetClientFactory(ctx)
	if err != nil {
		return nil, nil, err
	}

	hostURL := federationHost.AsURL()
	apClient, err := clientFactory.WithKeys(ctx, actionsUser, actionsUser.KeyID(), []*url.URL{&hostURL})
	if err != nil {
		return nil, nil, err
	}

	body, err := apClient.GetBody(personID.AsURI())
	if err != nil {
		return nil, nil, err
	}

	person := fm.ForgePerson{}
	err = person.UnmarshalJSON(body)
	if err != nil {
		return nil, nil, err
	}

	if res, err := validation.IsValid(person); !res {
		return nil, nil, err
	}

	localFqdn, err := url.ParseRequestURI(setting.AppURL)
	if err != nil {
		return nil, nil, err
	}

	personIDFromActor, err := fm.NewPersonID(person.ID.GetLink().String(), string(federationHost.NodeInfo.SoftwareName))
	if err != nil {
		return nil, nil, err
	}
	email := fmt.Sprintf("f%v@%v", uuid.New().String(), localFqdn.Hostname())
	loginName := personIDFromActor.AsLoginName()
	name := fmt.Sprintf("@%v%v", person.PreferredUsername.String(), personIDFromActor.HostSuffix())
	fullName := person.Name.String()

	if len(person.Name) == 0 {
		fullName = name
	}

	inbox, err := url.ParseRequestURI(person.Inbox.GetLink().String())
	if err != nil {
		return nil, nil, err
	}

	pubKeyBytes, err := decodePublicKeyPem(person.PublicKey.PublicKeyPem)
	if err != nil {
		return nil, nil, err
	}

	newUser := user_model.User{
		LowerName:                    strings.ToLower(name),
		Name:                         name,
		FullName:                     fullName,
		Email:                        email,
		EmailNotificationsPreference: "disabled",
		ProhibitLogin:                true,
		Passwd:                       "",
		Salt:                         "",
		PasswdHashAlgo:               "",
		LoginName:                    loginName,
		Type:                         user_model.UserTypeActivityPubUser,
		IsAdmin:                      false,
	}

	federatedUser := user_model.FederatedUser{
		ExternalID:            personIDFromActor.ID,
		FederationHostID:      federationHost.ID,
		InboxPath:             inbox.Path,
		NormalizedOriginalURL: personIDFromActor.AsURI(),
		KeyID: sql.NullString{
			String: person.PublicKey.ID.String(),
			Valid:  true,
		},
		PublicKey: sql.Null[sql.RawBytes]{
			V:     pubKeyBytes,
			Valid: true,
		},
	}

	log.Trace("Fetched person's %v federatedUser from distant server: %s", person, federatedUser.LogString())
	return &newUser, &federatedUser, nil
}

// ResolveRemoteUserHandle resolves a remote user handle (e.g. "@bob@forgejob:3000", "bob@forgejob:3000", or an ActivityPub actor URL)
// via WebFinger and/or ActivityPub, creates/materializes the user locally if not already present, and returns the local user record.
func ResolveRemoteUserHandle(ctx context.Context, query string) (*user_model.User, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("empty query")
	}

	// 1. If query is directly an HTTP/HTTPS URL:
	if strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") {
		u, _, _, err := FindOrCreateFederatedUser(ctx, query)
		return u, err
	}

	// 2. If query is a WebFinger-style handle (@user@host or user@host):
	handle := strings.TrimPrefix(query, "@")
	parts := strings.Split(handle, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid handle format %q", query)
	}

	username, host := parts[0], parts[1]
	if !setting.FederationHostAllowed(host) {
		return nil, fmt.Errorf("federation host %q is not allowed by policy", host)
	}

	schema := "https"
	if setting.Federation.InsecureAllowInvalidHosts || strings.Contains(host, "localhost") || strings.Contains(host, "127.0.0.1") || strings.Contains(host, ":") {
		schema = "http"
	}

	webfingerURL := fmt.Sprintf("%s://%s/.well-known/webfinger?resource=acct:%s@%s", schema, host, username, host)

	actionsUser := user_model.NewAPServerActor()
	clientFactory, err := activitypub.GetClientFactory(ctx)
	if err != nil {
		return nil, err
	}

	parsedURL, err := url.Parse(webfingerURL)
	if err != nil {
		return nil, err
	}

	client, err := clientFactory.WithKeys(ctx, actionsUser, actionsUser.KeyID(), []*url.URL{parsedURL})
	if err != nil {
		return nil, err
	}

	body, err := client.GetBody(webfingerURL)
	if err != nil {
		guessedActorURI := fmt.Sprintf("%s://%s/api/v1/activitypub/user-id/%s", schema, host, username)
		u, _, _, err2 := FindOrCreateFederatedUser(ctx, guessedActorURI)
		if err2 == nil {
			return u, nil
		}
		return nil, fmt.Errorf("webfinger request to %q failed: %w", webfingerURL, err)
	}

	var jrd struct {
		Subject string `json:"subject"`
		Links   []struct {
			Rel  string `json:"rel"`
			Type string `json:"type"`
			Href string `json:"href"`
		} `json:"links"`
	}

	if err := json.Unmarshal(body, &jrd); err != nil {
		return nil, fmt.Errorf("parsing webfinger response: %w", err)
	}

	var actorURI string
	for _, link := range jrd.Links {
		if link.Rel == "self" && (strings.Contains(link.Type, "activity") || strings.Contains(link.Type, "ld+json") || link.Type == "") {
			actorURI = link.Href
			break
		}
	}
	if actorURI == "" {
		for _, link := range jrd.Links {
			if link.Rel == "self" && link.Href != "" {
				actorURI = link.Href
				break
			}
		}
	}

	if actorURI == "" {
		return nil, fmt.Errorf("no self link found in webfinger response for %q", query)
	}

	u, _, _, err := FindOrCreateFederatedUser(ctx, actorURI)
	return u, err
}
