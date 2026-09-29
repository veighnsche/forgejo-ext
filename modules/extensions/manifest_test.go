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
