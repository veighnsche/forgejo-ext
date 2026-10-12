// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"strconv"
	"testing"
)

func TestBrowserContractVersion(t *testing.T) {
	if BrowserContractVersion != 1 {
		t.Fatalf("BrowserContractVersion = %d, want 1 (bump only with a coordinated contract change)", BrowserContractVersion)
	}
	if rendered := strconv.Itoa(BrowserContractVersion); rendered != "1" {
		t.Fatalf("BrowserContractVersion renders %q, want \"1\"", rendered)
	}
}
