// Copyright 2023 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed

import (
	"reflect"
	"unsafe"

	ap "github.com/go-ap/activitypub"
	"github.com/valyala/fastjson"
)

const (
	// RepositoryType is the ForgeFed Repository actor type.
	RepositoryType ap.ActivityVocabularyType = "Repository"
	// PushType is the ForgeFed Push activity type: new commits have been added
	// to a Repository (https://forgefed.org/ns#Push).
	PushType ap.ActivityVocabularyType = "Push"
)

type Repository struct {
	ap.Actor
	// Team Collection of actors who have management/push access to the repository
	Team ap.Item `jsonld:"team,omitempty"`
	// Forks OrderedCollection of repositories that are forks of this repository
	Forks ap.Item `jsonld:"forks,omitempty"`
	// ForkedFrom Identifies the repository which this repository was created as a fork
	ForkedFrom ap.Item `jsonld:"forkedFrom,omitempty"`
	// Mirrors Identifies the upstream repository this repository is a pull mirror of
	Mirrors ap.Item `jsonld:"mirrors,omitempty"`
	// MirroredBy OrderedCollection of repositories that are pull mirrors of this repository
	MirroredBy ap.Item `jsonld:"mirroredBy,omitempty"`
	// MirrorsTo Identifies the downstream repository this repository is a push mirror of
	MirrorsTo ap.Item `jsonld:"mirrorsTo,omitempty"`
	// MirroredFrom OrderedCollection of repositories that are push mirrors of this repository
	MirroredFrom ap.Item `jsonld:"mirroredFrom,omitempty"`
	// CloneURI The endpoint from which the repository content can be obtained via
	// the native git protocol (ForgeFed https://forgefed.org/ns#cloneUri)
	CloneURI string `jsonld:"cloneUri,omitempty"`
	// PushURI The endpoint(s) through which commits can be pushed via the native
	// git protocol (ForgeFed https://forgefed.org/ns#pushUri)
	PushURI ap.Item `jsonld:"pushUri,omitempty"`
}

// RepositoryNew initializes a Repository type actor
func RepositoryNew(id ap.ID) *Repository {
	a := ap.ActorNew(id, RepositoryType)
	a.Type = RepositoryType
	o := Repository{Actor: *a}
	return &o
}

func (r Repository) MarshalJSON() ([]byte, error) {
	b, err := r.Actor.MarshalJSON()
	if len(b) == 0 || err != nil {
		return nil, err
	}

	b = b[:len(b)-1]
	if r.Team != nil {
		ap.JSONWriteItemProp(&b, "team", r.Team)
	}
	if r.Forks != nil {
		ap.JSONWriteItemProp(&b, "forks", r.Forks)
	}
	if r.ForkedFrom != nil {
		ap.JSONWriteItemProp(&b, "forkedFrom", r.ForkedFrom)
	}
	if r.Mirrors != nil {
		ap.JSONWriteItemProp(&b, "mirrors", r.Mirrors)
	}
	if r.MirroredBy != nil {
		ap.JSONWriteItemProp(&b, "mirroredBy", r.MirroredBy)
	}
	if r.MirrorsTo != nil {
		ap.JSONWriteItemProp(&b, "mirrorsTo", r.MirrorsTo)
	}
	if r.MirroredFrom != nil {
		ap.JSONWriteItemProp(&b, "mirroredFrom", r.MirroredFrom)
	}
	if r.CloneURI != "" {
		ap.JSONWriteStringProp(&b, "cloneUri", r.CloneURI)
	}
	if r.PushURI != nil {
		ap.JSONWriteItemProp(&b, "pushUri", r.PushURI)
	}
	ap.JSONWrite(&b, '}')
	return b, nil
}

func JSONLoadRepository(val *fastjson.Value, r *Repository) error {
	if err := ap.OnActor(&r.Actor, func(a *ap.Actor) error {
		return ap.JSONLoadActor(val, a)
	}); err != nil {
		return err
	}

	r.Team = ap.JSONGetItem(val, "team")
	r.Forks = ap.JSONGetItem(val, "forks")
	r.ForkedFrom = ap.JSONGetItem(val, "forkedFrom")
	r.Mirrors = ap.JSONGetItem(val, "mirrors")
	r.MirroredBy = ap.JSONGetItem(val, "mirroredBy")
	r.MirrorsTo = ap.JSONGetItem(val, "mirrorsTo")
	r.MirroredFrom = ap.JSONGetItem(val, "mirroredFrom")
	r.CloneURI = string(val.GetStringBytes("cloneUri"))
	r.PushURI = ap.JSONGetItem(val, "pushUri")
	return nil
}

func (r *Repository) UnmarshalJSON(data []byte) error {
	p := fastjson.Parser{}
	val, err := p.ParseBytes(data)
	if err != nil {
		return err
	}
	return JSONLoadRepository(val, r)
}

// ToRepository tries to convert the it Item to a Repository Actor.
func ToRepository(it ap.Item) (*Repository, error) {
	switch i := it.(type) {
	case *Repository:
		return i, nil
	case Repository:
		return &i, nil
	case *ap.Actor:
		return (*Repository)(unsafe.Pointer(i)), nil
	case ap.Actor:
		return (*Repository)(unsafe.Pointer(&i)), nil
	default:
		// NOTE(marius): this is an ugly way of dealing with the interface conversion error: types from different scopes
		typ := reflect.TypeFor[*Repository]()
		if i, ok := reflect.ValueOf(it).Convert(typ).Interface().(*Repository); ok {
			return i, nil
		}
	}
	return nil, ap.ErrorInvalidType[ap.Actor](it)
}

type withRepositoryFn func(*Repository) error

// OnRepository calls function fn on it Item if it can be asserted to type *Repository
func OnRepository(it ap.Item, fn withRepositoryFn) error {
	if it == nil {
		return nil
	}
	ob, err := ToRepository(it)
	if err != nil {
		return err
	}
	return fn(ob)
}
