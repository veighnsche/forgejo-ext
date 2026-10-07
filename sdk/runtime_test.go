// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeRequestBoundsAndSingleObject(t *testing.T) {
	const maximumRequestBytes = 64 << 10
	type request struct {
		Value string `json:"value"`
	}
	valid := `{"value":"x"}`
	decode := func(body string, target *request) error {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		return decodeRequest(r, target)
	}

	var exact request
	if err := decode(valid+strings.Repeat(" ", maximumRequestBytes-len(valid)), &exact); err != nil || exact.Value != "x" {
		t.Fatalf("exact byte limit rejected: value=%q err=%v", exact.Value, err)
	}
	for name, body := range map[string]string{
		"limit plus one whitespace": valid + strings.Repeat(" ", maximumRequestBytes-len(valid)+1),
		"large trailing whitespace": valid + strings.Repeat(" ", 4*maximumRequestBytes),
		"second JSON value":         valid + `{}`,
		"unknown field":             `{"value":"x","extra":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := decode(body, &request{}); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}

	var duplicate request
	if err := decode(`{"value":"first","value":"last"}`, &duplicate); err != nil || duplicate.Value != "last" {
		t.Fatalf("duplicate-field behavior changed: value=%q err=%v", duplicate.Value, err)
	}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = io.NopCloser(&requestReadFailure{data: []byte(valid)})
	if err := decodeRequest(r, &request{}); err == nil {
		t.Fatal("request body read failure accepted")
	}
}

type requestReadFailure struct {
	data []byte
	done bool
}

func (r *requestReadFailure) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		return copy(p, r.data), nil
	}
	return 0, errors.New("synthetic read failure")
}
