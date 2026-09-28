package test

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"net/http"

	"github.com/42wim/httpsig"
)

var HttpsigAlgs []httpsig.Algorithm

const (
	// ActivityStreamsContentType const
	ActivityStreamsContentType = `application/ld+json; profile="https://www.w3.org/ns/activitystreams"`
	httpsigExpirationTime      = 60
)

func CreateFederationPostReq(body []byte, privateKey, pubID, to string) (req *http.Request, err error) {
	privPem, _ := pem.Decode([]byte(privateKey))
	privParsed, err := x509.ParsePKCS1PrivateKey(privPem.Bytes)
	if err != nil {
		return nil, err
	}

	algs := HttpsigAlgs
	digestAlg := httpsig.DigestAlgorithm("SHA-256")
	postHeaders := []string{"(request-target)", "Date", "Host", "Digest"}

	buf := bytes.NewBuffer(body)
	req, err = http.NewRequest(http.MethodPost, to, buf)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Accept", "application/json, "+ActivityStreamsContentType)
	req.Header.Add("Date", "Mon, 21 Sep 2026 08:56:24 GMT")
	req.Header.Add("Host", req.URL.Host)
	req.Header.Add("Content-Type", ActivityStreamsContentType)

	if pubID != "" {
		signer, _, err := httpsig.NewSigner(algs, digestAlg, postHeaders, httpsig.Signature, httpsigExpirationTime)
		if err != nil {
			return nil, err
		}
		if err := signer.SignRequest(privParsed, pubID, req, body); err != nil {
			return nil, err
		}
	}
	return req, err
}
