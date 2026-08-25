// Copyright 2024, 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

//nolint:dupl
package forgefed

import (
	"time"

	"forgejo.org/modules/validation"

	ap "github.com/go-ap/activitypub"
)

// ForgeUndoFollow activity data type: wraps an Undo whose object is a Follow,
// used to federate an unfollow.
// swagger:model
type ForgeUndoFollow struct {
	// swagger:ignore
	ap.Activity
}

// NewForgeUndoFollow builds an Undo(Follow) activity: the object is a nested
// Follow activity from the same actor targeting the same object.
func NewForgeUndoFollow(actorIRI, objectIRI string, startTime time.Time) (ForgeUndoFollow, error) {
	result := ForgeUndoFollow{}
	result.Type = ap.UndoType
	result.Actor = ap.IRI(actorIRI)
	result.StartTime = startTime

	follow := ap.Activity{}
	follow.Type = ap.FollowType
	follow.Actor = ap.IRI(actorIRI)
	follow.Object = ap.IRI(objectIRI)
	result.Object = &follow

	if valid, err := validation.IsValid(result); !valid {
		return ForgeUndoFollow{}, err
	}
	return result, nil
}

func (undo *ForgeUndoFollow) UnmarshalJSON(data []byte) error {
	return undo.Activity.UnmarshalJSON(data)
}

func (undo ForgeUndoFollow) MarshalJSON() ([]byte, error) {
	return undo.Activity.MarshalJSON()
}

func (undo ForgeUndoFollow) IsNewer(compareTo time.Time) bool {
	return undo.StartTime.After(compareTo)
}

func (undo ForgeUndoFollow) Validate() []string {
	var result []string
	result = append(result, validation.ValidateNotEmpty(undo.Type, "type")...)
	result = append(result, validation.ValidateOneOf(undo.Type, []any{ap.UndoType}, "type")...)

	if undo.Actor == nil {
		result = append(result, "Actor should not be nil.")
	} else {
		result = append(result, validation.ValidateNotEmpty(undo.Actor.GetID().String(), "actor")...)
	}

	result = append(result, validation.ValidateNotEmpty(undo.StartTime.String(), "startTime")...)
	if undo.StartTime.IsZero() {
		result = append(result, "StartTime was invalid.")
	}

	if undo.Object == nil {
		result = append(result, "object should not be empty.")
	} else if activity, ok := undo.Object.(*ap.Activity); !ok {
		result = append(result, "object is not of type Activity")
	} else {
		result = append(result, validation.ValidateNotEmpty(activity.Type, "type")...)
		result = append(result, validation.ValidateOneOf(activity.Type, []any{ap.FollowType}, "type")...)

		if activity.Actor == nil {
			result = append(result, "Object.Actor should not be nil.")
		} else {
			result = append(result, validation.ValidateNotEmpty(activity.Actor.GetID().String(), "actor")...)
		}

		if activity.Object == nil {
			result = append(result, "Object.Object should not be nil.")
		} else {
			result = append(result, validation.ValidateNotEmpty(activity.Object.GetID().String(), "object")...)
		}
	}
	return result
}
