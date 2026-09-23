// Copyright 2024, 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package federation

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"forgejo.org/models/forgefed"
	"forgejo.org/models/user"
	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"

	"github.com/42wim/httpsig"
	ap "github.com/go-ap/activitypub"
)

func FindOrCreateActorKey(ctx context.Context, keyID string) (pubKey any, err error) {
	log.Trace("KeyID: %v", keyID)
	keyURL, err := url.Parse(keyID)
	if err != nil {
		return nil, err
	}

	// Check for existing user key
	_, federatedUser, err := user.FindFederatedUserByKeyID(ctx, keyURL.String())
	if err != nil {
		if !user.IsErrFederatedUserNotExists(err) {
			return nil, err
		}

		// Check for existing federation host key
		federationHost, err := forgefed.FindFederationHostByKeyID(ctx, keyURL.String())
		if err != nil {
			if !forgefed.IsErrFederationHostNotFound(err) {
				return nil, err
			}
		} else if federationHost.PublicKey.Valid {
			pubKey, err := x509.ParsePKIXPublicKey(federationHost.PublicKey.V)
			if err != nil {
				return nil, err
			}

			return pubKey, nil
		}
	} else if federatedUser.PublicKey.Valid {
		pubKey, err := x509.ParsePKIXPublicKey(federatedUser.PublicKey.V)
		if err != nil {
			return nil, err
		}

		return pubKey, nil
	}

	port := uint16(443)
	if keyURL.Port() != "" {
		port64, err := strconv.ParseUint(keyURL.Port(), 10, 16)
		if err != nil {
			return nil, err
		}
		port = uint16(port64)
	}

	hostURL := *keyURL
	// if the `Actor` is not an `Application`, check for a `FederationHost` record associated with the key ID
	// for user `Actor`s, we should already have a `FederationHost` record in the database, so we ensure that the user `Actor` matches the respective `FederationHost` URL
	// otherwise, we just match against the supplied key ID URL
	if federationHost, err := forgefed.FindFederationHostByFqdnAndPort(ctx, keyURL.Hostname(), port); err == nil {
		hostURL = federationHost.AsURL()
	}

	// Fetch missing key
	pubKey, pubKeyBytes, actor, err := fetchKeyFromAp(ctx, *keyURL, hostURL)
	if err != nil {
		return nil, err
	}

	switch actor.Type {
	case ap.PersonType:
		_, federatedUser, _, err := FindOrCreateFederatedUser(ctx, actor.ID.String())
		if err != nil {
			return nil, err
		}

		err = updateFederatedUserKey(ctx, federatedUser, pubKeyBytes, actor)
		if err != nil {
			return nil, err
		}
	case ap.ApplicationType:
		federationHost, err := FindOrCreateFederationHost(ctx, actor.ID.String())
		if err != nil {
			return nil, err
		}

		err = updateFederationHostKey(ctx, federationHost, pubKeyBytes, actor)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("Fetched actortype (%s) is unhandled", actor.Type)
	}

	return pubKey, nil
}

func updateFederatedUserKey(ctx context.Context, federatedUser *user.FederatedUser, pubKeyBytes []byte, actor *ap.Actor) (err error) {
	if actor.Type != ap.PersonType {
		return fmt.Errorf("Fetched user type (%s) is not of user type Person", actor.Type)
	}

	if federatedUser.NormalizedOriginalURL != actor.ID.String() {
		return fmt.Errorf("federated user (%s) does not match the stored one %s", actor.ID, federatedUser.NormalizedOriginalURL)
	}

	federatedUser.KeyID = sql.NullString{
		String: actor.PublicKey.ID.String(),
		Valid:  true,
	}

	federatedUser.PublicKey = sql.Null[sql.RawBytes]{
		V:     pubKeyBytes,
		Valid: true,
	}

	return user.UpdateFederatedUser(ctx, federatedUser)
}

func updateFederationHostKey(ctx context.Context, federationHost *forgefed.FederationHost, pubKeyBytes []byte, actor *ap.Actor) (err error) {
	if actor.Type != ap.ApplicationType {
		return fmt.Errorf("Fetched user type (%s) is not of user type Application", actor.Type)
	}

	federationHost.KeyID = sql.NullString{
		String: actor.PublicKey.ID.String(),
		Valid:  true,
	}

	federationHost.PublicKey = sql.Null[sql.RawBytes]{
		V:     pubKeyBytes,
		Valid: true,
	}

	err = forgefed.UpdateFederationHost(ctx, federationHost)
	if err != nil {
		return err
	}

	return nil
}

func fetchKeyFromAp(ctx context.Context, keyURL, hostURL url.URL) (pubKey any, pubKeyBytes []byte, apPerson *ap.Actor, err error) {
	log.Trace("keyURL %v", keyURL)
	actionsUser := user.NewAPServerActor()

	clientFactory, err := activitypub.GetClientFactory(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	apClient, err := clientFactory.WithKeys(ctx, actionsUser, actionsUser.KeyID(), []*url.URL{&hostURL})
	if err != nil {
		return nil, nil, nil, err
	}

	b, err := apClient.GetBody(keyURL.String())
	if err != nil {
		return nil, nil, nil, err
	}

	actor := ap.ActorNew(ap.IRI(keyURL.String()), ap.ActorType)
	err = actor.UnmarshalJSON(b)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ActivityStreams object cannot be converted to actor: %w", err)
	}

	pubKeyFromAp := actor.PublicKey
	if pubKeyFromAp.PublicKeyPem == "" {
		return nil, nil, nil, ErrKeyNotFound{KeyID: keyURL.String()}
	}

	if pubKeyFromAp.ID.String() != keyURL.String() {
		return nil, nil, nil, fmt.Errorf("cannot find publicKey with id: %v in %v", keyURL, string(b))
	}

	pubKeyBytes, err = decodePublicKeyPem(pubKeyFromAp.PublicKeyPem)
	if err != nil {
		return nil, nil, nil, err
	}

	pubKey, err = x509.ParsePKIXPublicKey(pubKeyBytes)
	if err != nil {
		return nil, nil, nil, err
	}

	log.Trace("For %v fetched pubKey %v", keyURL, pubKey)
	return pubKey, pubKeyBytes, actor, err
}

func decodePublicKeyPem(pubKeyPem string) ([]byte, error) {
	block, _ := pem.Decode([]byte(pubKeyPem))
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("could not decode publicKeyPem to PUBLIC KEY pem block type")
	}

	return block.Bytes, nil
}

func getKeyID(r *http.Request) (string, error) {
	if !setting.Federation.SignatureEnforced {
		return "", fmt.Errorf("Signature Enforcing is not enabled")
	}

	v, err := httpsig.NewVerifier(r)
	if err != nil {
		log.Debug("For %q verification failed: %v", r.URL.Path, err)
	}

	keyURI := v.KeyId()

	return keyURI, nil
}

// Finds the ID of requester and actor and compares them
// This function should only be called **after** http signature verification
func VerifyKeyIDMatchesActorID(ctx context.Context, req *http.Request, activity *ap.Activity) error {
	// skip if key veryfication is not enforced
	if !setting.Federation.SignatureEnforced {
		return nil
	}

	keyID, err := getKeyID(req)
	if err != nil {
		return err
	}

	keyURL, err := url.Parse(keyID)
	if err != nil {
		return err
	}

	if activity.Actor == nil {
		return fmt.Errorf("Invalid getting actor from request")
	}

	actorURI := activity.Actor.GetLink().String()

	_, federatedUser, federationHost, err := FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		log.Error("Error finding or creating federated user (%s): %v", actorURI, err)
		return fmt.Errorf("Error finding or creating federated user (%s): %v", actorURI, err)
	}
	_, keyUser, err := user.FindFederatedUserByKeyID(ctx, keyURL.String())
	if err == nil && federatedUser.KeyID.String != keyUser.KeyID.String {
		return fmt.Errorf("KeyID (%v) in signature does not match FederatedUserID (%v)", keyUser.KeyID.String, federatedUser.KeyID.String)
	} else if err != nil && user.IsErrFederatedUserNotExists(err) {
		keyHost, err := forgefed.FindFederationHostByKeyID(ctx, keyURL.String())
		if err != nil {
			return err
		} else if federationHost.KeyID.String != keyHost.KeyID.String {
			return fmt.Errorf("KeyID (%v) in signature does not match FederationHostID (%v)", keyHost.KeyID.String, federationHost.KeyID.String)
		}
	} else {
		return err
	}
	return nil
}

func VerifyRequestDigest(req *http.Request) error {
	digest := req.Header.Get("Digest")
	if digest == "" {
		return fmt.Errorf("Error: no digest in Header")
	}

	digestSplit := regexp.MustCompile("=").Split(digest, 2)

	digestAlgo, err := matchCryptoAlgorithm(digestSplit[0])
	if err != nil {
		return err
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}

	req.Body = io.NopCloser(bytes.NewBuffer(body))

	h := digestAlgo.New()
	h.Write(body)

	calcDigest := base64.StdEncoding.EncodeToString(h.Sum(nil))

	if calcDigest != digestSplit[1] {
		return fmt.Errorf("Calculated digest does not match digest from header")
	}

	return nil
}

var crytoAlgoyithms = []crypto.Hash{
	crypto.MD4,
	crypto.MD5,
	crypto.SHA1,
	crypto.SHA224,
	crypto.SHA256,
	crypto.SHA384,
	crypto.SHA512,
	crypto.MD5SHA1,
	crypto.RIPEMD160,
	crypto.SHA3_224,
	crypto.SHA3_256,
	crypto.SHA3_384,
	crypto.SHA3_512,
	crypto.SHA512_224,
	crypto.SHA512_256,
	crypto.BLAKE2s_256,
	crypto.BLAKE2b_256,
	crypto.BLAKE2b_384,
	crypto.BLAKE2b_512,
}

func matchCryptoAlgorithm(algo string) (crypto.Hash, error) {
	for _, h := range crytoAlgoyithms {
		if strings.EqualFold(strings.Replace(algo, "_", "-", 1), h.String()) {
			return h, nil
		}
	}
	return crypto.Hash.HashFunc(21), fmt.Errorf("Unknown hash alogrithm: %s", algo)
}
