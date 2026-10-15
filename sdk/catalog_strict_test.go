// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"encoding/json"
	"strings"
	"testing"
)

func strictCatalogFixture() Catalog {
	return Catalog{Version: CatalogVersion, Packages: []CatalogPackage{
		{
			ID: "controller", Version: "1.0.0",
			URL:             "https://publisher.example/releases/download/v1/controller-1.0.0-linux-amd64.tar.gz",
			Size:            12345,
			SHA256:          strings.Repeat("a", 64),
			Required:        true,
			FountainProto:   Protocol,
			BrowserContract: BrowserContractVersion,
			Capabilities:    []string{CapabilityActorRead, CapabilityExtensionLifecycle},
		},
		{
			ID: "sample", Version: "1.0.0",
			URL:                 "https://publisher.example/releases/download/v1/sample-1.0.0-linux-amd64.tar.gz",
			Size:                12345,
			SHA256:              strings.Repeat("b", 64),
			Required:            false,
			FountainProto:       Protocol,
			BrowserContract:     BrowserContractVersion,
			Capabilities:        []string{CapabilityActorRead, CapabilityServiceBridge},
			LifecycleController: "controller",
		},
	}}
}

func mustEncodeCatalog(t *testing.T, catalog Catalog) []byte {
	t.Helper()
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDecodeCatalogStrictRoundTrip(t *testing.T) {
	decoded, err := DecodeCatalog(mustEncodeCatalog(t, strictCatalogFixture()))
	if err != nil {
		t.Fatalf("valid catalog rejected: %v", err)
	}
	if err := ValidateCatalog(decoded); err != nil {
		t.Fatalf("decoded catalog invalid: %v", err)
	}
	sample, ok := FindCatalogPackage(decoded, "sample")
	if !ok || sample.LifecycleController != "controller" {
		t.Fatalf("lifecycle controller did not survive the wire: %+v", sample)
	}
	controller, ok := FindCatalogPackage(decoded, "controller")
	if !ok || controller.LifecycleController != "" {
		t.Fatalf("required controller must omit its controller: %+v", controller)
	}
}

func TestDecodeCatalogRejectsStrayFields(t *testing.T) {
	raw := `{"version":1,"packages":[],"arch":"amd64"}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("stray envelope field accepted")
	}
	raw = `{"version":1,"packages":[{"id":"sample","version":"1.0.0","url":"https://publisher.example/x.tar.gz","size":10,"sha256":"` + strings.Repeat("b", 64) + `","required":false,"fountain_protocol":1,"browser_contract":1,"capabilities":[],"lifecycle_controller":"controller","digest":"zzz"}]}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("stray package field accepted")
	}
}

func TestDecodeCatalogRejectsDuplicateKeys(t *testing.T) {
	raw := `{"version":1,"version":1,"packages":[]}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("duplicate envelope key accepted")
	}
	raw = `{"version":1,"packages":[{"id":"sample","id":"sample","version":"1.0.0","url":"https://publisher.example/x.tar.gz","size":10,"sha256":"` + strings.Repeat("b", 64) + `","required":false,"fountain_protocol":1,"browser_contract":1,"capabilities":[]}]}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("duplicate package key accepted")
	}
}

func TestDecodeCatalogRejectsTrailingData(t *testing.T) {
	raw := string(mustEncodeCatalog(t, strictCatalogFixture())) + ` {}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("trailing data accepted")
	}
}

func TestDecodeCatalogRequiresPresence(t *testing.T) {
	// "required" omitted must not decode as an explicit false.
	raw := `{"version":1,"packages":[{"id":"sample","version":"1.0.0","url":"https://publisher.example/x.tar.gz","size":10,"sha256":"` + strings.Repeat("b", 64) + `","fountain_protocol":1,"browser_contract":1,"capabilities":[]}]}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("omitted required flag accepted")
	}
	raw = `{"packages":[]}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("omitted envelope version accepted")
	}
	raw = `{"version":1}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("omitted packages accepted")
	}
	// Wrong scalar types fail instead of coercing.
	raw = `{"version":"1","packages":[]}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("string envelope version accepted")
	}
	raw = `{"version":1,"packages":[{"id":"sample","version":"1.0.0","url":"https://publisher.example/x.tar.gz","size":10.5,"sha256":"` + strings.Repeat("b", 64) + `","required":false,"fountain_protocol":1,"browser_contract":1,"capabilities":[]}]}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("fractional size accepted")
	}
	raw = `{"version":1,"packages":[{"id":"sample","version":"1.0.0","url":"https://publisher.example/x.tar.gz","size":10,"sha256":"` + strings.Repeat("b", 64) + `","required":"false","fountain_protocol":1,"browser_contract":1,"capabilities":[]}]}`
	if _, err := DecodeCatalog([]byte(raw)); err == nil {
		t.Fatal("string required flag accepted")
	}
}

func TestValidateCatalogPackageFrozenShapes(t *testing.T) {
	entry := strictCatalogFixture().Packages[1]
	cases := map[string]func(*CatalogPackage){
		"plain http url":  func(e *CatalogPackage) { e.URL = "http://publisher.example/sample.tar.gz" },
		"query url":       func(e *CatalogPackage) { e.URL += "?token=secret" },
		"fragment url":    func(e *CatalogPackage) { e.URL += "#frag" },
		"userinfo url":    func(e *CatalogPackage) { e.URL = "https://user@publisher.example/sample.tar.gz" },
		"bad digest":      func(e *CatalogPackage) { e.SHA256 = "xyz" },
		"upper digest":    func(e *CatalogPackage) { e.SHA256 = strings.Repeat("B", 64) },
		"zero size":       func(e *CatalogPackage) { e.Size = 0 },
		"oversize":        func(e *CatalogPackage) { e.Size = MaximumArchiveBytes + 1 },
		"future protocol": func(e *CatalogPackage) { e.FountainProto = Protocol + 1 },
		"future browser":  func(e *CatalogPackage) { e.BrowserContract = BrowserContractVersion + 1 },
		"bad version":     func(e *CatalogPackage) { e.Version = ".1.0" },
		"long version":    func(e *CatalogPackage) { e.Version = strings.Repeat("v", 65) },
		"long id":         func(e *CatalogPackage) { e.ID = strings.Repeat("a", 65) },
		"bad id":          func(e *CatalogPackage) { e.ID = "../escape" },
		"unknown capability": func(e *CatalogPackage) {
			e.Capabilities = []string{"native.actor.read", "tailnet.admin"}
		},
		"duplicate capability": func(e *CatalogPackage) {
			e.Capabilities = []string{CapabilityActorRead, CapabilityActorRead}
		},
		"self controller": func(e *CatalogPackage) { e.LifecycleController = e.ID },
		"bad controller":  func(e *CatalogPackage) { e.LifecycleController = "Not A Slug!" },
	}
	for name, mutate := range cases {
		bad := entry
		bad.Capabilities = append([]string(nil), entry.Capabilities...)
		mutate(&bad)
		if err := ValidateCatalogPackage(bad); err == nil {
			t.Fatalf("invalid catalog entry accepted (%s)", name)
		}
	}
}

func TestValidateCatalogBounds(t *testing.T) {
	if err := ValidateCatalog(Catalog{Version: CatalogVersion + 1}); err == nil {
		t.Fatal("unknown catalog version accepted")
	}
	entry := strictCatalogFixture().Packages[1]
	duplicate := Catalog{Version: CatalogVersion, Packages: []CatalogPackage{entry, entry}}
	if err := ValidateCatalog(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate catalog package not reported: %v", err)
	}
	many := Catalog{Version: CatalogVersion}
	for i := 0; i < maximumCatalogPackages+1; i++ {
		many.Packages = append(many.Packages, CatalogPackage{
			ID:      "package-" + strings.Repeat("a", 8) + "-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)),
			Version: "1.0.0", URL: "https://publisher.example/x.tar.gz", Size: 1,
			SHA256: strings.Repeat("c", 64), FountainProto: Protocol, BrowserContract: BrowserContractVersion,
		})
	}
	if err := ValidateCatalog(many); err == nil {
		t.Fatal("oversized catalog accepted")
	}
}
