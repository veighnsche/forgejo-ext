// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package activitypub

import (
	"fmt"
	"net/http"
	"net/url"

	fedhost "forgejo.org/models/forgefed"
	"forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	app_context "forgejo.org/services/context"
	"forgejo.org/services/federation"

	"github.com/42wim/httpsig"
	ap "github.com/go-ap/activitypub"
)

func verifyHTTPSignature(ctx app_context.APIContext) (authenticated bool, err error) {
	if !setting.Federation.SignatureEnforced {
		return true, nil
	}

	r := ctx.Req

	// 1. Figure out what key we need to verify
	v, err := httpsig.NewVerifier(r)
	if err != nil {
		log.Debug("For %q verification failed: %v", r.URL.Path, err)
		return false, err
	}

	log.Debug("Verify %q, signed by KeyId: %v", r.URL.Path, v.KeyId())
	signatureAlgorithm := httpsig.Algorithm(setting.Federation.SignatureAlgorithms[0])
	pubKey, err := federation.FindOrCreateActorKey(ctx, v.KeyId())
	if err != nil {
		return false, err
	}

	err = v.Verify(pubKey, signatureAlgorithm)
	if err != nil {
		log.Debug("For %q verification failed: %v", r.URL.Path, err)
		return false, err
	}
	return true, nil
}

// ReqHTTPSignature function
func ReqHTTPSignature() func(ctx *app_context.APIContext) {
	return func(ctx *app_context.APIContext) {
		if authenticated, err := verifyHTTPSignature(*ctx); err != nil {
			log.Warn("verifyHttpSignature failed: %v", err)
			ctx.Error(http.StatusBadRequest, "reqSignature", "request signature verification failed")
		} else if !authenticated {
			ctx.Error(http.StatusForbidden, "reqSignature", "request signature verification failed")
		}
	}
}

func getKeyID(ctx app_context.APIContext) (string, error) {
	if !setting.Federation.SignatureEnforced {
		return "", fmt.Errorf("Signature Enforcing is not enabled")
	}

	r := ctx.Req
	v, err := httpsig.NewVerifier(r)
	if err != nil {
		log.Debug("For %q verification failed: %v", r.URL.Path, err)
	}

	keyURI := v.KeyId()

	return keyURI, nil
}

// Finds the ID of requester and actor and compares them
func verifyKeyIDMatchesActorID(ctx app_context.APIContext, activity *ap.Activity) error {
	// skip if key veryfication is not enforced
	if !setting.Federation.SignatureEnforced {
		return nil
	}

	keyID, err := getKeyID(ctx)
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

	_, federatedUser, federationHost, err := federation.FindOrCreateFederatedUser(ctx, actorURI)
	if err != nil {
		log.Error("Error finding or creating federated user (%s): %v", actorURI, err)
		return fmt.Errorf("Federated user not found: %v", err)
	}
	_, keyUser, err := user.FindFederatedUserByKeyID(ctx, keyURL.String())
	if err != nil {

		if !user.IsErrFederatedUserNotExists(err) {
			return err
		}

		keyHost, err := fedhost.FindFederationHostByKeyID(ctx, keyURL.String())
		if err != nil {
			if !fedhost.IsErrFederationHostNotFound(err) {
				return err
			}
		} else {
			if federationHost.KeyID.String != keyHost.KeyID.String {
				return fmt.Errorf("KeyID (%v) in signature does not match FederationHost ID (%v)", keyHost.KeyID.String, federationHost.KeyID.String)
			}
		}
	} else {
		if federatedUser.KeyID.String != keyUser.KeyID.String {
			return fmt.Errorf("KeyID (%v) in signature does not match FederatedUser ID (%v)", keyUser.KeyID.String, federatedUser.KeyID.String)
		}
	}
	return nil
}
