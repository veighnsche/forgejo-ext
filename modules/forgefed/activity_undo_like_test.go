// Copyright 2023, 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"forgejo.org/modules/forgefed"
	"forgejo.org/modules/validation"

	ap "github.com/go-ap/activitypub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_NewForgeUndoLikeFromActivity(t *testing.T) {
	startTime, _ := time.Parse("2006-Jan-02", "2024-Mar-27")
	activity := ap.Activity{
		Type:      ap.UndoType,
		Actor:     ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
		StartTime: startTime,
		Object: &ap.Activity{
			Type:   ap.LikeType,
			Actor:  ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
			Object: ap.IRI("https://codeberg.org/api/v1/activitypub/repository-id/1"),
		},
	}

	forgefedUndoLike, err := forgefed.NewForgeUndoLikeFromActivity(&activity)
	require.NoError(t, err)

	like, _ := forgefedUndoLike.Like()
	assert.Equal(t, "https://codeberg.org/api/v1/activitypub/repository-id/1", like.Object.GetLink().String())
}

func Test_NewForgeUndoLike(t *testing.T) {
	actorIRI := "https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"
	objectIRI := "https://codeberg.org/api/v1/activitypub/repository-id/1"
	want := []byte(`{"type":"Undo","startTime":"2024-03-27T00:00:00Z",` +
		`"actor":"https://repo.prod.meissa.de/api/v1/activitypub/user-id/1",` +
		`"object":{` +
		`"type":"Like",` +
		`"actor":"https://repo.prod.meissa.de/api/v1/activitypub/user-id/1",` +
		`"object":"https://codeberg.org/api/v1/activitypub/repository-id/1"}}`)

	like, err := forgefed.NewForgeLike(actorIRI, objectIRI, time.Time{})
	require.NoError(t, err)

	startTime, _ := time.Parse("2006-Jan-02", "2024-Mar-27")
	sut, err := forgefed.NewForgeUndoLike(like, startTime)
	require.NoError(t, err)

	got, err := sut.MarshalJSON()
	require.NoError(t, err)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MarshalJSON() got = %q, want %q", got, want)
	}
}

func Test_UndoLikeMarshalJSON(t *testing.T) {
	type testPair struct {
		item    forgefed.ForgeUndoLike
		want    []byte
		wantErr error
	}

	startTime, _ := time.Parse("2006-Jan-02", "2024-Mar-27")
	tests := map[string]testPair{
		"empty": {
			item: forgefed.ForgeUndoLike{},
			want: nil,
		},
		"valid": {
			item: forgefed.ForgeUndoLike{
				Activity: ap.Activity{
					StartTime: startTime,
					Actor:     ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
					Type:      ap.UndoType,
					Object: &ap.Activity{
						Type:   ap.LikeType,
						Actor:  ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
						Object: ap.IRI("https://codeberg.org/api/v1/activitypub/repository-id/1"),
					},
				},
			},
			want: []byte(`{"type":"Undo",` +
				`"startTime":"2024-03-27T00:00:00Z",` +
				`"actor":"https://repo.prod.meissa.de/api/v1/activitypub/user-id/1",` +
				`"object":{` +
				`"type":"Like",` +
				`"actor":"https://repo.prod.meissa.de/api/v1/activitypub/user-id/1",` +
				`"object":"https://codeberg.org/api/v1/activitypub/repository-id/1"}}`),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tt.item.MarshalJSON()
			if (err != nil || tt.wantErr != nil) && tt.wantErr.Error() != err.Error() {
				t.Errorf("MarshalJSON() error = \"%v\", wantErr \"%v\"", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MarshalJSON()\ngot = %q\nwant = %q", got, tt.want)
			}
		})
	}
}

func Test_UndoLikeUnmarshalJSON(t *testing.T) {
	type testPair struct {
		item    []byte
		want    *forgefed.ForgeUndoLike
		wantErr error
	}

	startTime, _ := time.Parse("2006-Jan-02", "2024-Mar-27")

	tests := map[string]testPair{
		"valid": {
			item: []byte(`{"type":"Undo",` +
				`"startTime":"2024-03-27T00:00:00Z",` +
				`"actor":"https://repo.prod.meissa.de/api/v1/activitypub/user-id/1",` +
				`"object":{` +
				`"type":"Like",` +
				`"actor":"https://repo.prod.meissa.de/api/v1/activitypub/user-id/1",` +
				`"object":"https://codeberg.org/api/v1/activitypub/repository-id/1"}}`),
			want: &forgefed.ForgeUndoLike{
				Activity: ap.Activity{
					StartTime: startTime,
					Actor:     ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
					Type:      ap.UndoType,
					Object: &ap.Activity{
						Type:   ap.LikeType,
						Actor:  ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
						Object: ap.IRI("https://codeberg.org/api/v1/activitypub/repository-id/1"),
					},
				},
			},
			wantErr: nil,
		},
		"invalid": {
			item:    []byte(`invalid JSON`),
			want:    nil,
			wantErr: errors.New("cannot parse JSON"),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := new(forgefed.ForgeUndoLike)
			err := got.UnmarshalJSON(test.item)
			if test.wantErr != nil {
				if err == nil {
					t.Errorf("UnmarshalJSON() error = nil, wantErr \"%v\"", test.wantErr)
				} else if !strings.Contains(err.Error(), test.wantErr.Error()) {
					t.Errorf("UnmarshalJSON() error = \"%v\", wantErr \"%v\"", err, test.wantErr)
				}
				return
			}
			remarshalledgot, _ := got.MarshalJSON()
			remarshalledwant, _ := test.want.MarshalJSON()
			if !reflect.DeepEqual(remarshalledgot, remarshalledwant) {
				t.Errorf("UnmarshalJSON() got = %#v\nwant %#v", got, test.want)
			}
		})
	}
}

func TestActivityValidationUndo(t *testing.T) {
	startTime, _ := time.Parse("2006-Jan-02", "2024-Mar-27")
	sut := forgefed.ForgeUndoLike{}
	sut.Type = ap.UndoType
	sut.Actor = ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1")
	sut.StartTime = startTime
	sut.Object = ap.Activity{
		Type:   ap.LikeType,
		Actor:  ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
		Object: ap.IRI("https://codeberg.org/api/v1/activitypub/repository-id/1"),
	}
	res, err := validation.IsValid(sut)
	require.NoError(t, err)
	assert.True(t, res)

	// valid even with not normalized actors
	sut = forgefed.ForgeUndoLike{}
	sut.Type = ap.UndoType
	sut.Actor = ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/USER-id/1")
	sut.StartTime = startTime
	sut.Object = ap.Activity{
		Type:   ap.LikeType,
		Actor:  ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
		Object: ap.IRI("https://codeberg.org/api/v1/activitypub/repository-id/1"),
	}
	res, err = validation.IsValid(sut)
	require.NoError(t, err)
	assert.True(t, res)

	sut = forgefed.ForgeUndoLike{}
	sut.Actor = ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1")
	sut.StartTime = startTime
	sut.Object = ap.Activity{
		Type:   ap.LikeType,
		Actor:  ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
		Object: ap.IRI("https://codeberg.org/api/v1/activitypub/repository-id/1"),
	}
	res, err = validation.IsValid(sut)
	require.ErrorIs(t, err, validation.ErrNotValid{Message: "forgefed.ForgeUndoLike: Value type should not be empty\nField type contains the value <nil>, which is not in allowed subset [Undo]"})
	assert.False(t, res)

	// actor missing
	sut = forgefed.ForgeUndoLike{}
	sut.Type = ap.UndoType
	sut.StartTime = startTime
	sut.Object = ap.Activity{
		Type:   ap.LikeType,
		Actor:  ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
		Object: ap.IRI("https://codeberg.org/api/v1/activitypub/repository-id/1"),
	}
	res, err = validation.IsValid(sut)
	require.ErrorIs(t, err, validation.ErrNotValid{Message: "forgefed.ForgeUndoLike: Field actor must not be nil"})
	assert.False(t, res)

	// like type missing
	sut = forgefed.ForgeUndoLike{}
	sut.Type = ap.UndoType
	sut.Actor = ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1")
	sut.StartTime = startTime
	sut.Object = ap.Activity{
		Actor:  ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
		Object: ap.IRI("https://codeberg.org/api/v1/activitypub/repository-id/1"),
	}
	res, err = validation.IsValid(sut)
	require.ErrorIs(t, err, validation.ErrNotValid{Message: "forgefed.ForgeUndoLike: Value object.type should not be empty\nField object.type contains the value <nil>, which is not in allowed subset [Like]"})
	assert.False(t, res)

	// like actor missing
	sut = forgefed.ForgeUndoLike{}
	sut.Type = ap.UndoType
	sut.Actor = ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1")
	sut.StartTime = startTime
	sut.Object = ap.Activity{
		Type:   ap.LikeType,
		Object: ap.IRI("https://codeberg.org/api/v1/activitypub/repository-id/1"),
	}
	res, err = validation.IsValid(sut)
	require.ErrorIs(t, err, validation.ErrNotValid{Message: "forgefed.ForgeUndoLike: Field object.actor must not be nil"})
	assert.False(t, res)

	// like object missing
	sut = forgefed.ForgeUndoLike{}
	sut.Type = ap.UndoType
	sut.Actor = ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1")
	sut.StartTime = startTime
	sut.Object = ap.Activity{
		Type:  ap.LikeType,
		Actor: ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1"),
	}
	res, err = validation.IsValid(sut)
	require.ErrorIs(t, err, validation.ErrNotValid{Message: "forgefed.ForgeUndoLike: Field object.object must not be nil"})
	assert.False(t, res)

	// undo actor <> like actor missing
	sut = forgefed.ForgeUndoLike{}
	sut.Type = ap.UndoType
	sut.Actor = ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/1")
	sut.StartTime = startTime
	sut.Object = ap.Activity{
		Type:   ap.LikeType,
		Actor:  ap.IRI("https://repo.prod.meissa.de/api/v1/activitypub/user-id/2"),
		Object: ap.IRI("https://codeberg.org/api/v1/activitypub/repository-id/1"),
	}
	res, err = validation.IsValid(sut)
	require.ErrorIs(t, err, validation.ErrNotValid{Message: "forgefed.ForgeUndoLike: The undo actor and undo.object actor has to be the same"})
	assert.False(t, res)
}
