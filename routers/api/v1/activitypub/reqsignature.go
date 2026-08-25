// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package activitypub

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	app_context "forgejo.org/services/context"
	"forgejo.org/services/federation"

	"github.com/42wim/httpsig"
)

// maxSignatureAge bounds how old (or far in the future) the signed Date
// header of a request may be for the signature to be accepted. It limits the
// replay window of captured signatures.
const maxSignatureAge = 12 * time.Hour

// signedHeadersRegex extracts the signed header list from the Signature (or
// Authorization: Signature) header, e.g. headers="(request-target) host date".
var signedHeadersRegex = regexp.MustCompile(`headers="([^"]*)"`)

// verifyHTTPSignature authenticates an incoming federation request:
//
//  1. the signature itself is parsed and verified against the key published
//     at the key id (NewVerifier also rejects expired (created)/(expires)
//     signature parameters);
//  2. the host of the key id must be allowed by the federation host policy,
//     before any network request is made to fetch the key;
//  3. the Date header must be covered by the signature and be recent, to
//     bound replay of captured requests;
//  4. for requests with a body, the Digest header must be signed and match
//     the received body;
//  5. the verified key is resolved to a FederationPrincipal which is stored
//     on the context: values claimed inside the request payload (e.g. an
//     activity's "actor") are untrusted until checked against it.
func verifyHTTPSignature(ctx *app_context.APIContext) error {
	r := ctx.Req

	v, err := httpsig.NewVerifier(r)
	if err != nil {
		log.Debug("For %q verification failed: %v", r.URL.Path, err)
		return err
	}

	keyID := v.KeyId()
	keyURL, err := url.Parse(keyID)
	if err != nil || keyURL.Host == "" {
		return fmt.Errorf("invalid keyId %q", keyID)
	}

	// The signing host must be allowed by the federation host policy before
	// any network request is made to fetch its key.
	if !setting.FederationHostAllowed(keyURL.Host) {
		return fmt.Errorf("federation host %q is not allowed by the host policy", keyURL.Host)
	}

	// The Date header must be signed and recent, bounding replay.
	if err := verifySignedDate(r); err != nil {
		return err
	}

	// Requests with a body must carry a matching signed Digest header.
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		if err := verifyBodyDigest(r); err != nil {
			return err
		}
	}

	pubKey, err := federation.FindOrCreateActorKey(ctx, keyID)
	if err != nil {
		return err
	}

	// Try each configured signature algorithm: peers may sign with any of
	// the advertised algorithms, not only the first one.
	var verifyErr error
	for _, algName := range setting.Federation.SignatureAlgorithms {
		if err := v.Verify(pubKey, httpsig.Algorithm(algName)); err == nil {
			verifyErr = nil
			break
		}
		verifyErr = err
	}
	if verifyErr != nil {
		log.Debug("For %q verification failed: %v", r.URL.Path, verifyErr)
		return verifyErr
	}

	// Resolve the verified identity and expose it to the handlers.
	principal, err := federation.ResolveFederationPrincipal(ctx, keyID)
	if err != nil {
		return err
	}
	log.Debug("Verified %q, signed by KeyId: %v (actor %q)", r.URL.Path, keyID, principal.ActorURI)
	ctx.SetFederationPrincipal(principal)
	return nil
}

// verifySignedDate enforces that the request's Date header is covered by the
// HTTP signature and lies within the acceptance window. Without this check,
// a captured signature could be replayed indefinitely.
func verifySignedDate(r *http.Request) error {
	sig := r.Header.Get("Signature")
	if sig == "" {
		sig = r.Header.Get("Authorization")
	}
	m := signedHeadersRegex.FindStringSubmatch(sig)
	if m == nil {
		return errors.New("http signature does not declare its signed headers")
	}
	signedDate := false
	for h := range strings.FieldsSeq(m[1]) {
		if strings.EqualFold(h, "date") {
			signedDate = true
			break
		}
	}
	if !signedDate {
		return errors.New("http signature does not cover the Date header")
	}
	date, err := http.ParseTime(r.Header.Get("Date"))
	if err != nil {
		return errors.New("request has an invalid or missing Date header")
	}
	if age := time.Since(date); age > maxSignatureAge || age < -maxSignatureAge {
		return fmt.Errorf("signed request date %v is outside the acceptance window", date)
	}
	return nil
}

// verifyBodyDigest enforces that the request body matches the Digest header.
// The Digest header is part of the signed headers, so this check (together
// with the signature) binds the request body cryptographically: neither the
// body nor the Digest header can be modified without invalidating the
// signature, and the body cannot be swapped while keeping the original
// Digest header. The body is read (bounded by the federation size limit) and
// restored so downstream handlers can read it again.
func verifyBodyDigest(r *http.Request) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, setting.Federation.MaxSize+1))
	if err != nil {
		return fmt.Errorf("reading request body: %w", err)
	}
	if int64(len(body)) > setting.Federation.MaxSize {
		return errors.New("request body exceeds the federation size limit")
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	digestHeader := r.Header.Get("Digest")
	if digestHeader == "" {
		return errors.New("request has no Digest header")
	}

	var expected string
	switch setting.Federation.DigestAlgorithm {
	case "SHA-512":
		sum := sha512.Sum512(body)
		expected = "SHA-512=" + base64.StdEncoding.EncodeToString(sum[:])
	default: // SHA-256 is the federation default
		sum := sha256.Sum256(body)
		expected = "SHA-256=" + base64.StdEncoding.EncodeToString(sum[:])
	}

	for part := range strings.SplitSeq(digestHeader, ",") {
		if strings.EqualFold(strings.TrimSpace(part), expected) {
			return nil
		}
	}
	return errors.New("request body does not match the signed Digest header")
}

// ReqHTTPSignature requires a validly signed federation request. Requests
// that fail any part of the verification are rejected; requests that pass
// expose their verified identity via ctx.FederationPrincipal().
func ReqHTTPSignature() func(ctx *app_context.APIContext) {
	return func(ctx *app_context.APIContext) {
		if err := verifyHTTPSignature(ctx); err != nil {
			log.Warn("verifyHttpSignature failed: %v", err)
			ctx.Error(http.StatusBadRequest, "reqSignature", "request signature verification failed")
		}
	}
}
