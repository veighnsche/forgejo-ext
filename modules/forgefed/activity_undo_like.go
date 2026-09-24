// Copyright 2023, 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed

import (
	"errors"
	"time"

	"forgejo.org/modules/validation"

	ap "github.com/go-ap/activitypub"
)

// ForgeLike activity data type
// swagger:model
type ForgeUndoLike struct {
	// swagger:ignore
	ap.Activity
}

func NewForgeUndoLikeFromActivity(activity *ap.Activity) (ForgeUndoLike, error) {
	like := activity.Object.(*ap.Activity)

	result := ForgeUndoLike{}
	result.Type = activity.Type
	result.Actor = activity.Actor
	result.Object = *like
	result.StartTime = activity.StartTime

	if valid, err := validation.IsValid(result); !valid {
		return ForgeUndoLike{}, err
	}
	return result, nil
}

func NewForgeUndoLike(like ForgeLike, startTime time.Time) (ForgeUndoLike, error) {
	result := ForgeUndoLike{}
	result.Type = ap.UndoType
	result.Actor = like.Actor
	result.StartTime = startTime
	result.Object = like

	if valid, err := validation.IsValid(result); !valid {
		return ForgeUndoLike{}, err
	}
	return result, nil
}

func (undo ForgeUndoLike) Like() (ap.Like, error) {
	like, ok := undo.Object.(ap.Like)
	if !ok {
		return ap.Like{}, errors.New("the object in the undo like is not a like object")
	}
	return like, nil
}

func (undo *ForgeUndoLike) UnmarshalJSON(data []byte) error {
	return undo.Activity.UnmarshalJSON(data)
}

func (undo ForgeUndoLike) Validate() []string {
	var result []string
	result = append(result, validation.ValidateNotEmpty(undo.Type, "type")...)
	result = append(result, validation.ValidateOneOf(undo.Type, []any{ap.UndoType}, "type")...)

	undoActorValidation := validation.ValidateIDExists(undo.Actor, "actor")
	result = append(result, undoActorValidation...)

	result = append(result, validation.ValidateNotEmpty(undo.StartTime.String(), "startTime")...)
	if undo.StartTime.IsZero() {
		result = append(result, "StartTime was invalid.")
	}

	like, err := undo.Like()
	if err != nil {
		result = append(result, "Invalid activity, error type assertion Like()")
	} else {
		result = append(result, validation.ValidateNotEmpty(like.Type, "object.type")...)
		result = append(result, validation.ValidateOneOf(like.Type, []any{ap.LikeType}, "object.type")...)

		undoLikeActorValidation := validation.ValidateIDExists(like.Actor, "object.actor")
		result = append(result, undoLikeActorValidation...)
		result = append(result, validation.ValidateIDExists(like.Object, "object.object")...)

		if len(undoActorValidation)+len(undoLikeActorValidation) == 0 {
			undoActor, err := NewActorID(undo.Actor.GetID().String())
			if err != nil {
				result = append(result, err.Error())
			}
			undoLikeActor, err := NewActorID(like.Actor.GetID().String())
			if err != nil {
				result = append(result, err.Error())
			}
			if undoActor.AsNormalizedURI() != undoLikeActor.AsNormalizedURI() {
				result = append(result, "The undo actor and undo.object actor has to be the same")
			}
		}
	}
	return result
}
