// Copyright 2022 The Gitea Authors. All rights reserved.
// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// TODO: Think about whether this should be moved to services/activitypub (compare to exosy/services/activitypub/client.go)
package activitypub

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	forgefed_model "forgejo.org/models/forgefed"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/hostmatcher"
	"forgejo.org/modules/log"
	"forgejo.org/modules/proxy"
	"forgejo.org/modules/setting"

	"github.com/42wim/httpsig"
	httpsign9421 "github.com/yaronf/httpsign"
)

const (
	// ActivityStreamsContentType const
	ActivityStreamsContentType = `application/ld+json; profile="https://www.w3.org/ns/activitystreams"`
	httpsigExpirationTime      = 60
)

func CurrentTime() string {
	return time.Now().UTC().Format(http.TimeFormat)
}

func containsRequiredHTTPHeaders(method string, headers []string) error {
	var hasRequestTarget, hasDate, hasDigest, hasHost bool
	for _, header := range headers {
		hasRequestTarget = hasRequestTarget || header == httpsig.RequestTarget
		hasDate = hasDate || header == "Date"
		hasDigest = hasDigest || header == "Digest"
		hasHost = hasHost || header == "Host"
	}
	if !hasRequestTarget {
		return fmt.Errorf("missing http header for %s: %s", method, httpsig.RequestTarget)
	} else if !hasDate {
		return fmt.Errorf("missing http header for %s: Date", method)
	} else if !hasHost {
		return fmt.Errorf("missing http header for %s: Host", method)
	} else if !hasDigest && method != http.MethodGet {
		return fmt.Errorf("missing http header for %s: Digest", method)
	}
	return nil
}

// Client struct
type ClientFactory struct {
	client      *http.Client
	algs        []httpsig.Algorithm
	digestAlgs  []httpsig.DigestAlgorithm
	getHeaders  []string
	postHeaders []string
}

// NewClient function
func NewClientFactory() (c *ClientFactory, err error) {
	return NewClientFactoryWithTimeout(5 * time.Second)
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	// NOTE: we don't want to allow any redirects for the ActivityPub client.
	// For our use-case there is no legitimate context for a redirect,
	// e.g. fetching keys, posting mailbox messages, etc.
	//
	// At some point in the future, we may want to support limited redirects,
	// possibly configurable through a settings option.
	return errors.New("activitypub: client: redirects are not allowed")
}

// NewClient function
func NewClientFactoryWithTimeout(timeout time.Duration) (c *ClientFactory, err error) {
	if err = containsRequiredHTTPHeaders(http.MethodGet, setting.Federation.GetHeaders); err != nil {
		return nil, err
	} else if err = containsRequiredHTTPHeaders(http.MethodPost, setting.Federation.PostHeaders); err != nil {
		return nil, err
	}

	digestAlgs := []httpsig.DigestAlgorithm{}
	for _, alg := range setting.Federation.DigestAlgorithms {
		digestAlgs = append(digestAlgs, httpsig.DigestAlgorithm(strings.ToUpper(alg)))
	}

	c = &ClientFactory{
		client: &http.Client{
			Transport: &http.Transport{
				Proxy: proxy.Proxy(),
			},
			Timeout:       timeout,
			CheckRedirect: checkRedirect,
		},
		algs:        setting.HttpsigAlgs,
		digestAlgs:  digestAlgs,
		getHeaders:  setting.Federation.GetHeaders,
		postHeaders: setting.Federation.PostHeaders,
	}
	return c, err
}

// SetHostMatcher sets the HTTP dialer that ensures the `to` host matches the set federation host.
//
// This prevents specially crafted key IDs or other IRIs from triggering a SSRF
// against hosts that do not match the host of the originating request.
//
// If no host is set, an error will be returned unless `setting.Federation.InsecureAllowInvalidHosts` is set to `true`.
func (cf *ClientFactory) setHostMatcher(hosts []*url.URL) error {
	if cf == nil {
		return errors.New("nil client factory")
	}

	hostsNil := len(hosts) == 0
	for _, host := range hosts {
		hostsNil = hostsNil || host == nil
	}

	if hostsNil && !setting.Federation.InsecureAllowInvalidHosts {
		return errors.New("nil client host(s)")
	}

	var hostMatchAllow, hostMatchBlock string
	if setting.Federation.InsecureAllowInvalidHosts {
		hostMatchAllow = fmt.Sprintf("%s, %s", hostmatcher.MatchBuiltinPrivate, hostmatcher.MatchBuiltinLoopback)
		for _, host := range hosts {
			if host != nil {
				hostMatchAllow = fmt.Sprintf("%s, %s", hostMatchAllow, host.Host)
			}
		}
	} else {
		for i, host := range hosts {
			if i == 0 {
				hostMatchAllow = host.Host
			} else {
				hostMatchAllow = fmt.Sprintf("%s, %s", hostMatchAllow, host.Host)
			}
		}
		hostMatchBlock = fmt.Sprintf("%s, %s", hostmatcher.MatchBuiltinPrivate, hostmatcher.MatchBuiltinLoopback)
	}

	allowMatcher := hostmatcher.ParseHostMatchList("", hostMatchAllow)
	blockMatcher := hostmatcher.ParseHostMatchList("", hostMatchBlock)
	dialCtx := hostmatcher.NewDialContext("activitypub", allowMatcher, blockMatcher, nil)

	cf.client.Transport = &http.Transport{
		Proxy:       proxy.Proxy(),
		DialContext: dialCtx,
	}

	return nil
}

type APClientFactory interface {
	WithKeys(ctx context.Context, user *user_model.User, pubID string, hosts []*url.URL) (APClient, error)
	WithKeysDirect(ctx context.Context, privateKey, pubID string, hosts []*url.URL) (APClient, error)
}

type ClientKeyType int

const (
	ClientKeyUser ClientKeyType = 1
	ClientKeyHost ClientKeyType = 2
)

// ClientPublicKey is a convenience struct used to construct a verifying ClientKey
type ClientPublicKey struct {
	KeyID string
	Alg   setting.Algorithm
	Type  ClientKeyType
}

// NewClientPublicKey creates a new ClientPublicKey for verifying signatures.
func NewClientPublicKey(keyID string, alg setting.Algorithm, ty ClientKeyType) ClientPublicKey {
	return ClientPublicKey{keyID, alg, ty}
}

func (k ClientPublicKey) PublicKeyBytes(ctx context.Context) (int64, []byte, error) {
	var err error

	switch k.Type {
	case ClientKeyHost:
		if host, err := forgefed_model.FindFederationHostByKeyID(ctx, k.KeyID); err == nil && host != nil && host.PublicKey.Valid {
			return host.ID, host.PublicKey.V, nil
		}
	case ClientKeyUser:
		fallthrough
	default:
		if _, federatedUser, err := user_model.FindFederatedUserByKeyID(ctx, k.KeyID); err == nil && federatedUser != nil && federatedUser.PublicKey.Valid {
			return federatedUser.UserID, federatedUser.PublicKey.V, nil
		}
	}

	return -1, nil, fmt.Errorf("error finding public key for key ID: %v, error: %w", k.KeyID, err)
}

// ClientKey creates a new [ClientKey].
func (k ClientPublicKey) ClientKey(ctx context.Context) (*ClientKey, error) {
	ownerID, keyBytes, err := k.PublicKeyBytes(ctx)
	if err != nil {
		return nil, err
	}
	clientKey := &ClientKey{
		pubKey:   keyBytes,
		pubKeyID: k.KeyID,
		ownerID:  ownerID,
		alg:      k.Alg,
	}
	return clientKey, nil
}

// ClientKey represents a client key used to sign a HTTP request.
type ClientKey struct {
	privKey  []byte
	pubKey   []byte
	pubKeyID string
	ownerID  int64
	alg      setting.Algorithm
}

// NewClientKey creates a new [ClientKey] from the provided parameters.
func NewClientKey(privKey []byte, pubKeyID string, alg setting.Algorithm) *ClientKey {
	return &ClientKey{
		privKey:  privKey,
		pubKeyID: pubKeyID,
		alg:      alg,
	}
}

// Client struct
type Client struct {
	client      *http.Client
	algs        []httpsig.Algorithm
	digestAlgs  []httpsig.DigestAlgorithm
	getHeaders  []string
	postHeaders []string
	priv        *rsa.PrivateKey
	pubID       string
	clientKeys  []*ClientKey
	useRFC9421  bool
}

// NewRequest function
func (cf *ClientFactory) WithKeysDirect(ctx context.Context, privateKey, pubID string, hosts []*url.URL) (APClient, error) {
	privPem, _ := pem.Decode([]byte(privateKey))
	privParsed, err := x509.ParsePKCS1PrivateKey(privPem.Bytes)
	if err != nil {
		return nil, err
	}

	if err = cf.setHostMatcher(hosts); err != nil {
		return nil, fmt.Errorf("client: invalid host for HostMatcher: %w", err)
	}

	clientKeys := []*ClientKey{
		NewClientKey([]byte(privateKey), pubID, setting.AlgorithmRSASHA256CAVAGE),
		NewClientKey([]byte(privateKey), pubID, setting.AlgorithmRSARFC9421),
	}

	c := Client{
		client:      cf.client,
		algs:        cf.algs,
		digestAlgs:  cf.digestAlgs,
		getHeaders:  cf.getHeaders,
		postHeaders: cf.postHeaders,
		priv:        privParsed,
		pubID:       pubID,
		clientKeys:  clientKeys,
		useRFC9421:  setting.Federation.UseRFC9421,
	}
	return &c, nil
}

func (cf *ClientFactory) WithKeys(ctx context.Context, user *user_model.User, pubID string, hosts []*url.URL) (APClient, error) {
	priv, err := GetPrivateKey(ctx, user)
	if err != nil {
		return nil, err
	}
	return cf.WithKeysDirect(ctx, priv, pubID, hosts)
}

// NewRequest function creates a new signed request to an external federation host.
func (c *Client) newRequest(method string, b []byte, to string) (req *http.Request, err error) {
	buf := bytes.NewBuffer(b)
	req, err = http.NewRequest(method, to, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/json, "+ActivityStreamsContentType)
	req.Header.Add("Date", CurrentTime())
	req.Header.Add("Host", req.URL.Host)
	req.Header.Add("User-Agent", "Gitea/"+setting.AppVer)
	req.Header.Add("Content-Type", ActivityStreamsContentType)

	return req, err
}

// Do makes an HTTP request to an ActivityPub server.
func (c *Client) Do(req *http.Request) (resp *http.Response, err error) {
	if req == nil {
		return nil, fmt.Errorf("nil ActivityPub request")
	}

	return c.client.Do(req)
}

// Post constructs a POST request with forgejo/gitea specific headers, and makes the request to the server.
func (c *Client) Post(b []byte, to string) (resp *http.Response, err error) {
	req, err := c.PostRequest(b, to)
	if err != nil {
		return nil, err
	}

	return c.Do(req)
}

// KeyID gets the key ID for the ActivityPub public key.
func (c *Client) KeyID() string {
	return c.pubID
}

// normalizeRFC9421Path normalizes RFC 9421 path:
//
// <https://www.rfc-editor.org/rfc/rfc9110#section-4.2.3>
func normalizeRFC9421Path(req *http.Request) {
	if req.URL.Path == "" {
		req.URL.Path = "/"
	}
}

// Create an http POST request with forgejo/gitea specific headers
func (c *Client) PostRequest(b []byte, to string) (req *http.Request, err error) {
	if req, err = c.newRequest(http.MethodPost, b, to); err != nil {
		return nil, err
	}

	if c.pubID != "" {
		if c.useRFC9421 {
			config := rfc9421SignConfig()

			hasBody := len(b) > 0
			sigHeaders := setting.Federation.PostHeadersRFC9421
			if hasBody {
				sigHeaders = append(sigHeaders, "Content-Digest")
			}
			fields := httpsign9421.Headers(sigHeaders...)

			signers, err := c.SignersRFC9421(config, fields)
			if err != nil {
				return nil, err
			}

			req.Header.Set("Created", fmt.Sprintf("%d", time.Now().Unix()))
			if hasBody {
				digest, err := ContentDigest(&req.Body, setting.Federation.DigestAlgorithms)
				if err != nil {
					return nil, err
				}
				req.Header.Set("Content-Digest", digest)
			}

			normalizeRFC9421Path(req)

			for i, signer := range signers {
				sigName := fmt.Sprintf("sig%d", i+1)
				signatureInput, signature, err := httpsign9421.SignRequest(sigName, signer, req)
				if err != nil {
					return nil, err
				}
				req.Header.Set("Signature-Input", signatureInput)
				req.Header.Set("Signature", signature)
			}
		} else {
			if len(c.digestAlgs) == 0 {
				return nil, fmt.Errorf("httpsig: nil digest algorithm")
			}
			signer, _, err := httpsig.NewSigner(c.algs, c.digestAlgs[0], c.postHeaders, httpsig.Signature, httpsigExpirationTime)
			if err != nil {
				return nil, err
			}
			if err := signer.SignRequest(c.priv, c.pubID, req, b); err != nil {
				return nil, err
			}
		}
	}

	return req, nil
}

// Create an http GET request with forgejo/gitea specific headers
func (c *Client) Get(to string) (resp *http.Response, err error) {
	req, err := c.GetRequest(to)
	if err != nil {
		return nil, err
	}

	return c.Do(req)
}

// GetRequest creates an http GET request with forgejo/gitea specific headers
func (c *Client) GetRequest(to string) (req *http.Request, err error) {
	if req, err = c.newRequest(http.MethodGet, nil, to); err != nil {
		return nil, err
	}

	if c.pubID != "" {
		if c.useRFC9421 {
			config := rfc9421SignConfig()
			fields := httpsign9421.Headers(setting.Federation.GetHeadersRFC9421...)

			signers, err := c.SignersRFC9421(config, fields)
			if err != nil {
				return nil, err
			}

			req.Header.Set("Created", fmt.Sprintf("%d", time.Now().Unix()))

			normalizeRFC9421Path(req)

			for i, signer := range signers {
				sigName := fmt.Sprintf("sig%d", i+1)
				signatureInput, signature, err := httpsign9421.SignRequest(sigName, signer, req)
				if err != nil {
					return nil, err
				}
				req.Header.Set("Signature-Input", signatureInput)
				req.Header.Set("Signature", signature)
			}
		} else {
			if len(c.digestAlgs) == 0 {
				return nil, fmt.Errorf("httpsig: nil digest algorithm")
			}
			signer, _, err := httpsig.NewSigner(c.algs, c.digestAlgs[0], c.getHeaders, httpsig.Signature, httpsigExpirationTime)
			if err != nil {
				return nil, err
			}
			if err := signer.SignRequest(c.priv, c.pubID, req, nil); err != nil {
				return nil, err
			}
		}
	}

	return req, nil
}

// Create an http GET request with forgejo/gitea specific headers
func (c *Client) GetBody(uri string) ([]byte, error) {
	response, err := c.Get(uri)
	if err != nil {
		return nil, err
	}
	log.Debug("Client: got status: %v", response.Status)
	if response.StatusCode != 200 {
		err = fmt.Errorf("got non 200 status code for id: %v", uri)
		return nil, err
	}
	defer response.Body.Close()
	if response.ContentLength > setting.Federation.MaxSize {
		return nil, fmt.Errorf("Request returned %d bytes (max allowed incoming size: %d bytes)", response.ContentLength, setting.Federation.MaxSize)
	} else if response.ContentLength == -1 {
		log.Warn("Request to %v returned an unknown content length, response may be truncated to %d bytes", uri, setting.Federation.MaxSize)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, setting.Federation.MaxSize))
	if err != nil {
		return nil, err
	}

	log.Debug("Client: got body: %v", charLimiter(string(body), 120))
	return body, nil
}

// SetRFC9421 sets whether to sign requests using the RFC 9421 HTTP Message Signing algorithm.
func (c *Client) SetRFC9421(use bool) {
	c.useRFC9421 = use
}

// GetRFC9421 gets whether the RFC 9421 HTTP Message Signing algorithm is used to sign requests.
func (c *Client) GetRFC9421() bool {
	return c.useRFC9421
}

// Limit number of characters in a string (useful to prevent log injection attacks and overly long log outputs)
// Thanks to https://www.socketloop.com/tutorials/golang-characters-limiter-example
func charLimiter(s string, limit int) string {
	reader := strings.NewReader(s)
	buff := make([]byte, limit)
	n, _ := io.ReadAtLeast(reader, buff, limit)
	if n != 0 {
		return fmt.Sprint(string(buff), "...")
	}
	return s
}

type APClient interface {
	newRequest(method string, b []byte, to string) (req *http.Request, err error)
	Post(b []byte, to string) (resp *http.Response, err error)
	PostRequest(b []byte, to string) (req *http.Request, err error)
	Get(to string) (resp *http.Response, err error)
	GetRequest(to string) (req *http.Request, err error)
	GetBody(uri string) ([]byte, error)
	Do(req *http.Request) (resp *http.Response, err error)
	GetRFC9421() bool
	SetRFC9421(use bool)
	KeyID() string
	SignedHeaders(method string, hasBody bool) string
}

// contextKey is a value for use with context.WithValue.
type contextKey struct {
	name string
}

// clientFactoryContextKey is a context key. It is used with context.Value() to get the current Food for the context
var (
	clientFactoryContextKey                 = &contextKey{"clientFactory"}
	_                       APClientFactory = &ClientFactory{}
)

// Context represents an activitypub client factory context
type Context struct {
	context.Context
	e APClientFactory
}

func NewContext(ctx context.Context, e APClientFactory) *Context {
	return &Context{
		Context: ctx,
		e:       e,
	}
}

// APClientFactory represents an activitypub client factory
func (ctx *Context) APClientFactory() APClientFactory {
	return ctx.e
}

// provides APClientFactory
type GetAPClient interface {
	GetClientFactory() APClientFactory
}

// GetClientFactory will get an APClientFactory from this context or returns the default implementation
func GetClientFactory(ctx context.Context) (APClientFactory, error) {
	if e := getClientFactory(ctx); e != nil {
		return e, nil
	}
	return NewClientFactory()
}

// getClientFactory will get an APClientFactory from this context or return nil
func getClientFactory(ctx context.Context) APClientFactory {
	if clientFactory, ok := ctx.(APClientFactory); ok {
		return clientFactory
	}
	clientFactoryInterface := ctx.Value(clientFactoryContextKey)
	if clientFactoryInterface != nil {
		return clientFactoryInterface.(GetAPClient).GetClientFactory()
	}
	return nil
}
