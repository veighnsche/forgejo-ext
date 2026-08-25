// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed_test

import (
	"errors"
	"testing"
	"time"

	"forgejo.org/modules/forgefed"
	"forgejo.org/modules/validation"

	ap "github.com/go-ap/activitypub"
)

func Test_NewForgeUndoFollow(t *testing.T) {
	actorIRI := "https://repo.example.com/api/v1/activitypub/user-id/1"
	objectIRI := "https://codeberg.example.com/api/v1/activitypub/user-id/2"
	startTime := time.Date(2024, 3, 27, 0, 0, 0, 0, time.UTC)

	sut, err := forgefed.NewForgeUndoFollow(actorIRI, objectIRI, startTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sut.Type != ap.UndoType {
		t.Errorf("expected type Undo, got %v", sut.Type)
	}
	if sut.Actor.GetID().String() != actorIRI {
		t.Errorf("expected actor %s, got %s", actorIRI, sut.Actor.GetID().String())
	}

	// The object must be a nested Follow activity.
	inner, ok := sut.Object.(*ap.Activity)
	if !ok {
		t.Fatalf("expected object to be an Activity, got %T", sut.Object)
	}
	if inner.Type != ap.FollowType {
		t.Errorf("expected nested object type Follow, got %v", inner.Type)
	}
	if inner.Actor.GetID().String() != actorIRI {
		t.Errorf("expected nested actor %s, got %s", actorIRI, inner.Actor.GetID().String())
	}
	if inner.Object.GetID().String() != objectIRI {
		t.Errorf("expected nested object %s, got %s", objectIRI, inner.Object.GetID().String())
	}

	if isValid, err := validation.IsValid(sut); !isValid {
		t.Errorf("ForgeUndoFollow should be valid: %v", err)
	}

	// Round-trip through JSON.
	data, err := sut.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var parsed forgefed.ForgeUndoFollow
	if err := parsed.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if parsed.Type != ap.UndoType {
		t.Errorf("round-trip type mismatch: %v", parsed.Type)
	}
	parsedInner, ok := parsed.Object.(*ap.Activity)
	if !ok || parsedInner.Type != ap.FollowType {
		t.Errorf("round-trip nested object is not a Follow: %v", parsed.Object)
	}
}

func Test_ForgeUndoFollowIsNewer(t *testing.T) {
	base := time.Date(2024, 3, 27, 0, 0, 0, 0, time.UTC)
	sut, err := forgefed.NewForgeUndoFollow(
		"https://example.com/ap/1", "https://example.com/ap/2", base,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !sut.IsNewer(base.Add(-time.Hour)) {
		t.Error("expected activity to be newer than a timestamp in the past")
	}
	if sut.IsNewer(base.Add(time.Hour)) {
		t.Error("expected activity not to be newer than a timestamp in the future")
	}
}

func Test_NewForgeUndoFollowInvalid(t *testing.T) {
	// A missing nested object must fail validation.
	sut := forgefed.ForgeUndoFollow{}
	sut.Type = ap.UndoType
	sut.Actor = ap.IRI("https://example.com/ap/1")
	_, err := validation.IsValid(sut)
	if err == nil {
		t.Error("expected validation error for missing nested object")
	}

	// The nested object must be a Follow, not e.g. a Like.
	if _, err := forgefed.NewForgeUndoFollow("", "", time.Now()); err == nil {
		t.Error("expected error for empty actor/object")
	}

	// Comparing errors when object is a non-Follow activity.
	invalid := forgefed.ForgeUndoFollow{}
	invalid.Type = ap.UndoType
	invalid.Actor = ap.IRI("https://example.com/ap/1")
	invalid.Object = &ap.Activity{Type: ap.LikeType}
	_, err = validation.IsValid(invalid)
	if err == nil {
		t.Error("expected validation error when nested object is not a Follow")
	}
	_ = errors.New // keep errors import used if refactored
}
