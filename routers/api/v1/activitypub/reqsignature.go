// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package activitypub

import (
	"fmt"
	"net/http"

	"forgejo.org/modules/activitypub"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	app_context "forgejo.org/services/context"
	"forgejo.org/services/federation"

	"github.com/42wim/httpsig"
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

func verifyRFC9421Signature(ctx app_context.APIContext) (authenticated bool, err error) {
	if !setting.Federation.SignatureEnforced {
		return true, nil
	}

	if err = federation.FetchRFC9421PubKey(ctx.Req); err != nil {
		return false, fmt.Errorf("error fetching RFC 9421 public key: %v", err)
	}

	for _, ty := range []activitypub.ClientKeyType{activitypub.ClientKeyUser, activitypub.ClientKeyHost} {
		if _, err = activitypub.VerifyPubKeyRFC9421(ctx.Req, ty); err == nil {
			return true, nil
		}
	}

	log.Warn("Error verifying RFC 9421 signature: %w", err)
	return false, err
}

// ReqHTTPSignature function
func ReqHTTPSignature() func(ctx *app_context.APIContext) {
	return func(ctx *app_context.APIContext) {
		var (
			authenticated bool
			err           error
		)

		if len(ctx.Req.Header.Get("Signature-Input")) != 0 {
			// RFC9421 includes the `Signature-Input` header,
			// draft-cavage-http-signatures does not
			authenticated, err = verifyRFC9421Signature(*ctx)
		} else {
			authenticated, err = verifyHTTPSignature(*ctx)
		}

		if err != nil {
			log.Warn("verifyHttpSignatures failed: %v", err)
			ctx.Error(http.StatusBadRequest, "reqSignature", "request signature verification failed")
		} else if !authenticated {
			ctx.Error(http.StatusForbidden, "reqSignature", "request signature verification failed")
		}
	}
}
