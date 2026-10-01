// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"strings"
	"testing"
)

func TestSplitOrdinaryResource(t *testing.T) {
	family, resource, ok := splitOrdinaryResource("ord:ref-write/1/file/refs/heads/main/0123456789abcdef")
	if !ok || family != "ref-write" || resource != "1/file/refs/heads/main" {
		t.Fatalf("split = %q %q %t, want ref-write 1/file/refs/heads/main true", family, resource, ok)
	}

	if _, _, ok := splitOrdinaryResource("cond:installation/operation"); ok {
		t.Fatal("conditional owner must not parse as an ordinary resource")
	}
	if _, _, ok := splitOrdinaryResource("ord:familyonly"); ok {
		t.Fatal("owner without a resource must not parse")
	}
	if _, _, ok := splitOrdinaryResource(""); ok {
		t.Fatal("empty owner must not parse")
	}
}

// TestRepositoryResourceFormats pins the owner resource shapes that
// offline recovery parses back: three segments for ref writes,
// four for lifecycle operations and protection rules, with slashes
// permitted only in the trailing segment.
func TestRepositoryResourceFormats(t *testing.T) {
	refWrite := RefWriteResource(7, RefWriteFile, "wiki/refs/heads/master")
	parts := strings.SplitN(refWrite, "/", 3)
	if len(parts) != 3 || parts[0] != "7" || parts[1] != RefWriteFile || parts[2] != "wiki/refs/heads/master" {
		t.Fatalf("ref-write resource %q splits to %#v", refWrite, parts)
	}

	lifecycle := LifecycleResource(0, LifecycleMigrate, "soda-tester", "demo")
	parts = strings.SplitN(lifecycle, "/", 4)
	if len(parts) != 4 || parts[0] != "0" || parts[1] != LifecycleMigrate || parts[2] != "soda-tester" || parts[3] != "demo" {
		t.Fatalf("lifecycle resource %q splits to %#v", lifecycle, parts)
	}

	protection := ProtectionResource(9, ProtectionBranch, ProtectionEdit, "feature/*")
	parts = strings.SplitN(protection, "/", 4)
	if len(parts) != 4 || parts[0] != "9" || parts[1] != ProtectionBranch || parts[2] != ProtectionEdit || parts[3] != "feature/*" {
		t.Fatalf("protection resource %q splits to %#v", protection, parts)
	}
}
