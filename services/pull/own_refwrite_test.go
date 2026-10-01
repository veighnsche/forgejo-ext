// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestRefWriteScopeWireContract pins the pull-local ownership mirror to
// the native-operation scope wire shape: the same JSON field names the
// service scope decodes, so offline recovery reads pull claims exactly
// like service claims. The mirror exists only to break the pull to
// native-operation import cycle; any drift here is a recovery bug.
func TestRefWriteScopeWireContract(t *testing.T) {
	raw, err := json.Marshal(refWriteScope{
		Kind:         "ordinary",
		Family:       familyRefWrite,
		RepositoryID: 7,
		Ref:          "refs/heads/main",
		OldOID:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		NewOID:       "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		PRNumber:     3,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"kind":          "ordinary",
		"family":        "ref-write",
		"repository_id": float64(7),
		"ref":           "refs/heads/main",
		"old_oid":       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"new_oid":       "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"pr_number":     float64(3),
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("mirror scope JSON = %v, want %v", decoded, want)
	}

	if got := refWriteResource(7, refWriteMerge, "refs/heads/main"); got != "7/merge/refs/heads/main" {
		t.Fatalf("mirror resource = %q, want %q", got, "7/merge/refs/heads/main")
	}
}
