// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestValidatePublishPayload(t *testing.T) {
	valid := PublishPayload{
		Ref:                   "refs/heads/candidate",
		ExpectedOld:           "absent",
		NewOID:                strings.Repeat("1", 40),
		ComparisonRef:         "refs/heads/master",
		ExpectedComparisonOID: strings.Repeat("2", 40),
	}
	if err := ValidatePublishPayload(valid); err != nil {
		t.Fatalf("valid creation payload refused: %v", err)
	}

	update := valid
	update.ExpectedOld = strings.Repeat("3", 40)
	update.Correction = &PublishCorrection{Number: 7, ExpectedAuthorID: 2}
	if err := ValidatePublishPayload(update); err != nil {
		t.Fatalf("valid correction payload refused: %v", err)
	}

	cases := map[string]PublishPayload{
		"same ref and comparison": func() PublishPayload { p := valid; p.Ref = "refs/heads/master"; return p }(),
		"no-op tuple":             func() PublishPayload { p := valid; p.ExpectedOld = strings.Repeat("1", 40); return p }(),
		"creation with PR": func() PublishPayload {
			p := valid
			p.Correction = &PublishCorrection{Number: 7, ExpectedAuthorID: 2}
			return p
		}(),
		"short OID": func() PublishPayload { p := valid; p.NewOID = "short"; return p }(),
	}
	for name, payload := range cases {
		if err := ValidatePublishPayload(payload); !errors.Is(err, ErrInvalidPublishPayload) {
			t.Fatalf("%s: expected ErrInvalidPublishPayload, got %v", name, err)
		}
	}
}

func TestPublishPushEnv(t *testing.T) {
	admission := strings.Repeat("a", 43)
	env, err := PublishPushEnv("op-1", admission)
	if err != nil {
		t.Fatalf("PublishPushEnv refused: %v", err)
	}
	want := []string{
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=X-Forgejo-Operation: op-1",
		"GIT_CONFIG_KEY_1=http.extraHeader",
		"GIT_CONFIG_VALUE_1=X-Forgejo-Extension-Admission: " + admission,
	}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("PublishPushEnv = %q, want %q", env, want)
	}

	if _, err := PublishPushEnv("op 1", admission); err == nil {
		t.Fatal("expected malformed operation ID to refuse")
	}
	if _, err := PublishPushEnv("op-1", "short"); err == nil {
		t.Fatal("expected malformed admission to refuse")
	}
}

func TestFormatPublishRefspec(t *testing.T) {
	refspec, err := FormatPublishRefspec("refs/heads/local", "refs/heads/candidate")
	if err != nil {
		t.Fatalf("FormatPublishRefspec refused: %v", err)
	}
	if refspec != "refs/heads/local:refs/heads/candidate" {
		t.Fatalf("unexpected refspec %q", refspec)
	}
	if _, err := FormatPublishRefspec("refs/heads/local", "master"); err == nil {
		t.Fatal("expected short target ref to refuse")
	}
}
