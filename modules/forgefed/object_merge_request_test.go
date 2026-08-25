// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed_test

import (
	"testing"

	fm "forgejo.org/modules/forgefed"
	"forgejo.org/modules/validation"

	ap "github.com/go-ap/activitypub"
)

// Test_ForgeMergeRequestParseViaHook verifies that an Offer(MergeRequest)
// activity parses through the ForgeFed custom-type hook, preserving the
// ForgeFed-specific fields the generic go-ap Object parser would drop.
func Test_ForgeMergeRequestParseViaHook(t *testing.T) {
	offerJSON := []byte(`{"type":"Offer","actor":"https://remote.example/ap/u/15","target":"https://local.example/api/v1/activitypub/repository-id/2","object":{"type":"MergeRequest","id":"https://remote.example/mr/1","source":"https://local.example/api/v1/activitypub/repository-id/2","sourceGitURL":"https://local.example/user2/repo2.git","sourceBranch":"feature-x","ref":"master","name":"Federated pull request","content":"desc"}}`)

	var offer ap.Activity
	if err := offer.UnmarshalJSON(offerJSON); err != nil {
		t.Fatalf("unmarshal offer: %v", err)
	}

	// The object must be parsed as a ForgeMergeRequest via the custom-type hook.
	var mr fm.ForgeMergeRequest
	if err := fm.OnMergeRequest(offer.Object, func(m *fm.ForgeMergeRequest) error {
		mr = *m
		return nil
	}); err != nil {
		t.Fatalf("object is not a ForgeMergeRequest: %v (got %T)", err, offer.Object)
	}

	if mr.GetType() != fm.MergeRequestType {
		t.Errorf("wrong type: %v", mr.GetType())
	}
	if mr.SourceGitURL != "https://local.example/user2/repo2.git" {
		t.Errorf("sourceGitURL lost: %q", mr.SourceGitURL)
	}
	if mr.SourceBranch != "feature-x" {
		t.Errorf("sourceBranch lost: %q", mr.SourceBranch)
	}
	if mr.Ref != "master" {
		t.Errorf("ref lost: %q", mr.Ref)
	}
	if mr.Name.String() != "Federated pull request" {
		t.Errorf("name lost: %q", mr.Name.String())
	}
	if mr.Content.String() != "desc" {
		t.Errorf("content lost: %q", mr.Content.String())
	}
	if string(mr.Source) != "https://local.example/api/v1/activitypub/repository-id/2" {
		t.Errorf("source lost: %q", mr.Source)
	}

	if isValid, err := validation.IsValid(mr); !isValid {
		t.Errorf("merge request should be valid: %v", err)
	}
}
