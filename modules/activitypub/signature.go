// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package activitypub

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"

	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"

	httpsign9421 "github.com/yaronf/httpsign"
)

// RSAPrivateKey attempts to parse an RSA private key from the [ClientKey].
func (k ClientKey) RSAPrivateKey() (*rsa.PrivateKey, error) {
	if k.privKey == nil {
		return nil, fmt.Errorf("nil private key")
	}

	switch k.alg {
	case setting.AlgorithmRSARFC9421, setting.AlgorithmRSAPSSRFC9421, setting.AlgorithmRSASHA256CAVAGE, setting.AlgorithmRSASHA512CAVAGE:
		privPem, _ := pem.Decode(k.privKey)
		return x509.ParsePKCS1PrivateKey(privPem.Bytes)
	default:
		return nil, fmt.Errorf("invalid signing algorithm: %v", k.alg)
	}
}

// RSAPublicKey attempts to parse an RSA private key from the [ClientKey].
func (k ClientKey) RSAPublicKey() (*rsa.PublicKey, error) {
	if k.privKey == nil {
		switch k.alg {
		case setting.AlgorithmRSARFC9421, setting.AlgorithmRSAPSSRFC9421, setting.AlgorithmRSASHA256CAVAGE, setting.AlgorithmRSASHA512CAVAGE:
			key, err := x509.ParsePKIXPublicKey(k.pubKey)
			if err != nil {
				return nil, err
			}
			pk, ok := key.(*rsa.PublicKey)
			if !ok {
				return nil, fmt.Errorf("invalid RSA public key")
			}
			return pk, nil
		default:
			return nil, fmt.Errorf("invalid signing algorithm: %v", k.alg)
		}
	} else {
		priv, err := k.RSAPrivateKey()
		if err != nil {
			return nil, err
		}
		pk, ok := priv.Public().(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("invalid RSA public key")
		}
		return pk, nil
	}
}

// ECDSAPrivateKey attempts to parse an ECDSA private key from the [ClientKey].
func (k ClientKey) ECDSAPrivateKey() (*ecdsa.PrivateKey, error) {
	if k.privKey == nil {
		return nil, fmt.Errorf("nil private key")
	}

	switch k.alg {
	case setting.AlgorithmP256CAVAGE, setting.AlgorithmP256RFC9421, setting.AlgorithmP384CAVAGE, setting.AlgorithmP384RFC9421:
		privPem, _ := pem.Decode(k.privKey)
		pk, err := x509.ParsePKCS8PrivateKey(privPem.Bytes)
		if err != nil {
			return nil, err
		}
		ecdsaPriv, ok := pk.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("invalid ECDSA private key")
		}
		return ecdsaPriv, nil
	default:
		return nil, fmt.Errorf("invalid ECDSA algorithm: %v", k.alg)
	}
}

// ECDSAPublicKey attempts to parse an ECDSA public key from the [ClientKey].
func (k ClientKey) ECDSAPublicKey() (*ecdsa.PublicKey, error) {
	if k.privKey == nil {
		pk, err := x509.ParsePKIXPublicKey(k.pubKey)
		if err != nil {
			return nil, err
		}

		switch k.alg {
		case setting.AlgorithmP256CAVAGE, setting.AlgorithmP256RFC9421,
			setting.AlgorithmP384CAVAGE, setting.AlgorithmP384RFC9421:
			k, ok := pk.(*ecdsa.PublicKey)
			if !ok {
				return nil, fmt.Errorf("invalid ECDSA public key")
			}
			return k, nil
		default:
			return nil, fmt.Errorf("invalid ECDSA algorithm: %v", k.alg)
		}
	} else {
		priv, err := k.ECDSAPrivateKey()
		if err != nil {
			return nil, err
		}
		pk, ok := priv.Public().(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("invalid ECDSA public key")
		}
		return pk, nil
	}
}

// Ed25519PrivateKey attempts to parse an Ed25519 private key from the [ClientKey].
func (k ClientKey) Ed25519PrivateKey() (*ed25519.PrivateKey, error) {
	if k.privKey == nil {
		return nil, fmt.Errorf("nil private key")
	}

	switch k.alg {
	case setting.AlgorithmEd25519:
		privPem, _ := pem.Decode(k.privKey)
		pk, err := x509.ParsePKCS8PrivateKey(privPem.Bytes)
		if err != nil {
			return nil, err
		}
		k, ok := pk.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("invalid Ed25519 private key")
		}
		return &k, nil
	default:
		return nil, fmt.Errorf("invalid Ed25519 algorithm: %v", k.alg)
	}
}

// Ed25519PublicKey attempts to parse an Ed25519 public key from the [ClientKey].
func (k ClientKey) Ed25519PublicKey() (*ed25519.PublicKey, error) {
	if k.privKey == nil {
		switch k.alg {
		case setting.AlgorithmEd25519:
			pk, err := x509.ParsePKIXPublicKey(k.pubKey)
			if err != nil {
				return nil, err
			}

			k, ok := pk.(ed25519.PublicKey)
			if !ok {
				return nil, fmt.Errorf("invalid Ed25519 public key")
			}
			return &k, nil
		default:
			return nil, fmt.Errorf("invalid Ed25519 algorithm: %v", k.alg)
		}
	} else {
		priv, err := k.Ed25519PrivateKey()
		if err != nil {
			return nil, err
		}
		pk, ok := priv.Public().(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("invalid Ed25519 public key")
		}
		return &pk, nil
	}
}

// OwnerID gets the owner ID for the ClientKey
//
// For federated user keys, this will be the local user ID.
// For federated host keys, this will be the federation host ID.
func (k ClientKey) OwnerID() int64 {
	return k.ownerID
}

type RFC9421Config struct {
	Signer   *httpsign9421.SignConfig
	Verifier *httpsign9421.VerifyConfig
}

func (k ClientKey) SignerRFC9421Ed25519(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Signer, error) {
	pk, err := k.Ed25519PrivateKey()
	if err != nil {
		return nil, err
	}
	return httpsign9421.NewEd25519Signer(*pk, config.Signer, fields)
}

func (k ClientKey) VerifierRFC9421Ed25519(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Verifier, error) {
	pk, err := k.Ed25519PublicKey()
	if err != nil {
		return nil, err
	}
	return httpsign9421.NewEd25519Verifier(*pk, config.Verifier, fields)
}

func (k ClientKey) SignerRFC9421HMACSHA256(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Signer, error) {
	if k.privKey == nil {
		return nil, fmt.Errorf("ClientKey: nil HMAC-SHA256 signing key")
	}
	return httpsign9421.NewHMACSHA256Signer(k.privKey, config.Signer, fields)
}

func (k ClientKey) VerifierRFC9421HMACSHA256(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Verifier, error) {
	return httpsign9421.NewHMACSHA256Verifier(k.pubKey, config.Verifier, fields)
}

func (k ClientKey) SignerRFC9421P256(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Signer, error) {
	pk, err := k.ECDSAPrivateKey()
	if err != nil {
		return nil, err
	}
	return httpsign9421.NewP256Signer(*pk, config.Signer, fields)
}

func (k ClientKey) VerifierRFC9421P256(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Verifier, error) {
	pk, err := k.ECDSAPublicKey()
	if err != nil {
		return nil, err
	}
	return httpsign9421.NewP256Verifier(*pk, config.Verifier, fields)
}

func (k ClientKey) SignerRFC9421P384(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Signer, error) {
	pk, err := k.ECDSAPrivateKey()
	if err != nil {
		return nil, err
	}
	return httpsign9421.NewP384Signer(*pk, config.Signer, fields)
}

func (k ClientKey) VerifierRFC9421P384(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Verifier, error) {
	pk, err := k.ECDSAPublicKey()
	if err != nil {
		return nil, err
	}
	return httpsign9421.NewP384Verifier(*pk, config.Verifier, fields)
}

func (k ClientKey) SignerRFC9421RSA(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Signer, error) {
	pk, err := k.RSAPrivateKey()
	if err != nil {
		return nil, err
	}
	return httpsign9421.NewRSASigner(*pk, config.Signer, fields)
}

func (k ClientKey) VerifierRFC9421RSA(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Verifier, error) {
	pk, err := k.RSAPublicKey()
	if err != nil {
		return nil, err
	}
	return httpsign9421.NewRSAVerifier(*pk, config.Verifier, fields)
}

func (k ClientKey) SignerRFC9421RSAPSS(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Signer, error) {
	pk, err := k.RSAPrivateKey()
	if err != nil {
		return nil, err
	}
	return httpsign9421.NewRSAPSSSigner(*pk, config.Signer, fields)
}

func (k ClientKey) VerifierRFC9421RSAPSS(config RFC9421Config, fields httpsign9421.Fields) (*httpsign9421.Verifier, error) {
	pk, err := k.RSAPublicKey()
	if err != nil {
		return nil, err
	}
	return httpsign9421.NewRSAPSSVerifier(*pk, config.Verifier, fields)
}

// SignerRFC9421 attempts to create a valid RFC 9421 HTTP Message signer.
func (k ClientKey) SignerRFC9421(config *httpsign9421.SignConfig, fields httpsign9421.Fields) (*httpsign9421.Signer, error) {
	if config == nil {
		return nil, fmt.Errorf("nil signer config")
	}

	config.SetKeyID(k.pubKeyID)

	signConfig := RFC9421Config{
		Signer: config,
	}

	switch k.alg {
	case setting.AlgorithmEd25519:
		return k.SignerRFC9421Ed25519(signConfig, fields)
	case setting.AlgorithmHMACSHA256:
		return k.SignerRFC9421HMACSHA256(signConfig, fields)
	case setting.AlgorithmP256RFC9421:
		return k.SignerRFC9421P256(signConfig, fields)
	case setting.AlgorithmP384RFC9421:
		return k.SignerRFC9421P384(signConfig, fields)
	case setting.AlgorithmRSARFC9421:
		return k.SignerRFC9421RSA(signConfig, fields)
	case setting.AlgorithmRSAPSSRFC9421:
		return k.SignerRFC9421RSAPSS(signConfig, fields)
	default:
		return nil, fmt.Errorf("invalid RFC 9421 signature algorithm: %v", k.alg)
	}
}

// VerifierRFC9421 attempts to create a valid RFC 9421 HTTP Message verifier.
func (k ClientKey) VerifierRFC9421(config *httpsign9421.VerifyConfig, fields httpsign9421.Fields) (*httpsign9421.Verifier, error) {
	if config == nil {
		return nil, fmt.Errorf("nil verifier config")
	}

	config.SetKeyID(k.pubKeyID)

	verifyConfig := RFC9421Config{
		Verifier: config,
	}

	switch k.alg {
	case setting.AlgorithmEd25519:
		return k.VerifierRFC9421Ed25519(verifyConfig, fields)
	case setting.AlgorithmHMACSHA256:
		return k.VerifierRFC9421HMACSHA256(verifyConfig, fields)
	case setting.AlgorithmP256RFC9421:
		return k.VerifierRFC9421P256(verifyConfig, fields)
	case setting.AlgorithmP384RFC9421:
		return k.VerifierRFC9421P384(verifyConfig, fields)
	case setting.AlgorithmRSARFC9421:
		return k.VerifierRFC9421RSA(verifyConfig, fields)
	case setting.AlgorithmRSAPSSRFC9421:
		return k.VerifierRFC9421RSAPSS(verifyConfig, fields)
	default:
		return nil, fmt.Errorf("invalid RFC 9421 signature algorithm: %v", k.alg)
	}
}

// SignersRFC9421 attempts to create a list of valid RFC 9421 HTTP Message signers.
func (c *Client) SignersRFC9421(config *httpsign9421.SignConfig, fields httpsign9421.Fields) ([]httpsign9421.Signer, error) {
	if config == nil {
		return nil, fmt.Errorf("nil signer config")
	}
	var (
		err     error
		signer  *httpsign9421.Signer
		signers []httpsign9421.Signer
	)
	for _, key := range c.clientKeys {
		signer, err = key.SignerRFC9421(config, fields)
		if err != nil {
			log.Debug("Invalid RFC 9421 signer: %v", err)
			continue
		}
		signers = append(signers, *signer)
	}

	return signers, nil
}

// VerifyPubKeyRFC9421 attempts to verify an HTTP request with RFC 9421 signature headers.
//
// It returns the public key for the first valid signature.
func VerifyPubKeyRFC9421(r *http.Request, clientKeyType ClientKeyType) (*ClientKey, error) {
	if r == nil {
		return nil, fmt.Errorf("nil request")
	}

	sigHeaders := r.Header.Values("Signature")
	if len(sigHeaders) == 0 {
		return nil, fmt.Errorf("missing signature header")
	}

	hasBody := r.Header.Get("Content-Digest") != ""

	var fields httpsign9421.Fields
	var headers []string
	if hasBody {
		headers = append(headers, "Content-Digest")
	}
	switch r.Method {
	case http.MethodGet:
		headers = append(headers, setting.Federation.GetHeadersRFC9421...)
	case http.MethodPost:
		headers = append(headers, setting.Federation.PostHeadersRFC9421...)
	default:
		return nil, fmt.Errorf("unsupported request type: %v", r.Method)
	}

	fields = httpsign9421.Headers(headers...)
	sigNames, err := httpsign9421.RequestSignatureNames(r, false)
	if err != nil {
		return nil, fmt.Errorf("error getting signature names: %w", err)
	}

	if len(sigNames) == 0 {
		return nil, fmt.Errorf("no RFC 9421 signature headers found: %v", r.Header)
	}

	config := httpsign9421.NewVerifyConfig()
	config.SetAllowedAlgs(setting.Federation.SignatureAlgorithmsRFC9421)

	if hasBody {
		if err = ValidateContentDigest(r.Header.Get("Content-Digest"), &r.Body, setting.Federation.DigestAlgorithms); err != nil {
			return nil, fmt.Errorf("invalid HTTP Content-Digest header: %v", err)
		}
	}

	ctx := r.Context()

	normalizeRFC9421Path(r)

	for _, name := range sigNames {
		msgDetails, err := httpsign9421.RequestDetails(name, r)
		if err != nil {
			log.Warn("no details for signature: %v, error: %v", name, err)
			continue
		}

		alg, err := setting.AlgorithmFromString(msgDetails.Alg)
		if err != nil {
			log.Debug("invalid HTTP message signature algorithm: %v", err)
			continue
		}

		if msgDetails.KeyID == nil {
			log.Warn("signature: %s nil key ID", name)
			continue
		}
		keyID := *msgDetails.KeyID

		log.Debug("verifyHttpMessageSignatures signature: %v, alg: %v, key ID: %v", name, alg, keyID)

		clientKey, err := NewClientPublicKey(keyID, alg, clientKeyType).ClientKey(ctx)
		if err != nil {
			log.Warn("error creating client key: %v", err)
			continue
		}
		verifier, err := clientKey.VerifierRFC9421(config, fields)
		if err != nil {
			log.Warn("error creating HTTP message signature verifier: %v", err)
			continue
		}
		if err = httpsign9421.VerifyRequest(name, *verifier, r); err == nil {
			// return on first verified signature
			return clientKey, nil
		}
		log.Debug("error validating signature %s with key ID: %s", name, keyID)
	}

	return nil, fmt.Errorf("no valid HTTP Message Signature (RFC 9421) found")
}

// ContentDigest calculates the SHA256 + SHA512 content-digest header value.
func ContentDigest(b *io.ReadCloser, digestAlgs []string) (string, error) {
	digest, err := httpsign9421.GenerateContentDigestHeader(b, digestAlgs)
	if err != nil {
		return "", fmt.Errorf("error generating RFC 9421 content-digest: %v", err)
	}
	return digest, nil
}

// ValidateContentDigest validates the `Content-Digest` header.
//
// `digestAlgs` represents the list of expected digest algorithms, e.g. "sha-256", "sha-512"
func ValidateContentDigest(header string, b *io.ReadCloser, digestAlgs []string) error {
	if b == nil {
		return fmt.Errorf("nil content-digest body I/O reader")
	}
	body := bytes.NewBufferString("")
	if _, err := body.ReadFrom(*b); err != nil {
		return err
	}
	*b = io.NopCloser(bytes.NewBufferString(body.String()))
	bodyReader := io.NopCloser(body)

	return httpsign9421.ValidateContentDigestHeader([]string{header}, &bodyReader, digestAlgs)
}

// SignedHeaders gets the signed HTTP headers.
func (c *Client) SignedHeaders(method string, hasBody bool) string {
	var ret string

	switch method {
	case http.MethodGet:
		if c.GetRFC9421() {
			headers := setting.Federation.GetHeadersRFC9421
			ret = fmt.Sprintf(`"%v"`, strings.Join(headers, `" "`))
		} else {
			ret = strings.Join(setting.Federation.GetHeaders, " ")
		}
	case http.MethodPost:
		if c.GetRFC9421() {
			headers := setting.Federation.PostHeadersRFC9421
			if hasBody {
				headers = append(headers, "Content-Digest")
			}
			ret = fmt.Sprintf(`"%v"`, strings.Join(headers, `" "`))
		} else {
			ret = strings.Join(setting.Federation.PostHeaders, " ")
		}
	default:
		ret = ""
	}

	return strings.ToLower(ret)
}

// rfc9421SignConfig is a helper function to create a valid RFC 9421 signer configuration.
func rfc9421SignConfig() *httpsign9421.SignConfig {
	return httpsign9421.NewSignConfig().SignCreated(true)
}
