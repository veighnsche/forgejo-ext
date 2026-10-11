// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT
package extensions

import (
	"strings"
	"testing"
)

func TestIssueGraphPayload(t *testing.T) {
	valid := func() IssueGraphPayload {
		return IssueGraphPayload{Creates: []IssueGraphCreate{{Key: "root", Title: "Objective", Body: "Outcome"}, {Key: "child", Title: "Child", Body: "Outcome"}}, Dependencies: []IssueGraphDependency{{Issue: IssueGraphRef{Key: "child"}, Prerequisite: IssueGraphRef{Key: "root"}}}}
	}
	if err := ValidateIssueGraphPayload(valid()); err != nil {
		t.Fatal(err)
	}
	cases := []IssueGraphPayload{{}, valid(), valid(), valid(), valid(), valid(), valid()}
	cases[1].Creates[1].Key = "root"
	cases[2].Dependencies[0].Issue.Key = "missing"
	cases[3].Dependencies[0].Issue.IssueID = 3
	cases[4].Dependencies[0].Prerequisite.Key = "child"
	cases[5].Dependencies[0].Remove = true
	cases[6].Creates[0].Body = strings.Repeat("x", MaxIssueGraphBodyBytes+1)
	for i, p := range cases {
		if ValidateIssueGraphPayload(p) == nil {
			t.Fatalf("accepted invalid case %d", i)
		}
	}
}
