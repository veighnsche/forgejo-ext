package util

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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

// Matches crypto algorythm from string
func MatchCryptoAlgorithm(algo string) (crypto.Hash, error) {
	for _, h := range crytoAlgoyithms {
		if strings.EqualFold(strings.Replace(algo, "_", "-", 1), h.String()) {
			return h, nil
		}
	}
	return crypto.Hash.HashFunc(21), fmt.Errorf("Unknown hash alogrithm: %s", algo)
}

// Calculates the reqest content-digest using the algorythm specified in the digest header
// and compares it with the value from the header
func VerifyRequestDigest(req *http.Request) error {
	// skip if Get request
	if req.Method == "GET" || req.Method == "" {
		return nil
	}

	digest := req.Header.Get("Digest")
	if digest == "" {
		return fmt.Errorf("Error: no digest in Header")
	}

	digestAlgoStr, digestStr, found := strings.Cut(digest, "=")
	if !found {
		return fmt.Errorf("Error: invalid digest header: %s", digest)
	}

	digestAlgo, err := MatchCryptoAlgorithm(digestAlgoStr)
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

	if calcDigest != digestStr {
		return fmt.Errorf("Calculated digest does not match digest from header")
	}

	return nil
}
