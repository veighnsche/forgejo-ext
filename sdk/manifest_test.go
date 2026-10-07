// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifest(t *testing.T) {
	packageDir := t.TempDir()
	valid := `{"protocol":1,"id":"sample-extension","name":"Sample","version":"1.0.0","executable":"bin/sample","pages":[{"id":"home","title":"Home","scope":"repository","entry":"main.js","permission":"read"}]}`
	if err := os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadManifest(packageDir)
	if err != nil || manifest.ID != "sample-extension" || manifest.Pages[0].Entry != "main.js" {
		t.Fatalf("valid manifest: %+v, %v", manifest, err)
	}
	for _, edit := range []struct{ old, replacement string }{
		{`"protocol":1`, `"protocol":2`},
		{`"executable":"bin/sample"`, `"executable":"../sample"`},
		{`"entry":"main.js"`, `"entry":"../main.js"`},
		{`"permission":"read"`, `"permission":"owner"`},
		{`"id":"sample-extension"`, `"id":"../sample"`},
	} {
		invalid := strings.Replace(valid, edit.old, edit.replacement, 1)
		if err := os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadManifest(packageDir); err == nil {
			t.Fatalf("accepted invalid manifest: %s", invalid)
		}
	}
}

func TestLoadManifestBoundsAndSingleObject(t *testing.T) {
	const maximumManifestBytes = 1 << 20
	packageDir := t.TempDir()
	valid := `{"protocol":1,"id":"sample-extension","name":"Sample","version":"1.0.0","executable":"bin/sample"}`
	write := func(contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write(valid + strings.Repeat(" ", maximumManifestBytes-len(valid)))
	if manifest, err := LoadManifest(packageDir); err != nil || manifest.ID != "sample-extension" {
		t.Fatalf("exact byte limit rejected: manifest=%+v err=%v", manifest, err)
	}
	for name, contents := range map[string]string{
		"limit plus one whitespace": valid + strings.Repeat(" ", maximumManifestBytes-len(valid)+1),
		"large trailing whitespace": valid + strings.Repeat(" ", 4*maximumManifestBytes),
		"second JSON value":         valid + `{}`,
		"unknown field":             strings.TrimSuffix(valid, "}") + `,"extra":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			write(contents)
			if _, err := LoadManifest(packageDir); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}

	write(`{"protocol":1,"id":"sample-extension","name":"First","name":"Last","version":"1.0.0","executable":"bin/sample"}`)
	if manifest, err := LoadManifest(packageDir); err != nil || manifest.Name != "Last" {
		t.Fatalf("duplicate-field behavior changed: manifest=%+v err=%v", manifest, err)
	}
	manifestPath := filepath.Join(packageDir, "extension.json")
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifestPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(packageDir); err == nil {
		t.Fatal("manifest read failure accepted")
	}
}
