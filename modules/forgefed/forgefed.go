// Copyright 2023, 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed

import (
	ap "github.com/go-ap/activitypub"
	"github.com/valyala/fastjson"
)

const ForgeFedNamespaceURI = "https://forgefed.org/ns"

// init wires the ForgeFed custom types (Repository, MergeRequest, ...) into
// the go-ap decoder: ItemTyperFunc instantiates them and JSONItemUnmarshal
// loads their JSON-LD, preserving the ForgeFed-specific fields that the
// generic go-ap Object parser would drop. Known types are delegated back to
// go-ap's defaults.
func init() {
	ap.ItemTyperFunc = GetItemByType
	ap.JSONItemUnmarshal = JSONUnmarshalerFn
	ap.IsNotEmpty = NotEmpty
}

// GetItemByType instantiates a new ForgeFed object if the type matches
func GetItemByType(typ ap.Typer) (ap.Item, error) {
	switch {
	case ap.ActivityVocabularyTypes{RepositoryType}.Match(typ):
		return RepositoryNew(""), nil
	case ap.ActivityVocabularyTypes{MergeRequestType}.Match(typ):
		return &ForgeMergeRequest{}, nil
	default:
		return ap.GetItemByType(typ)
	}
}

// JSONUnmarshalerFn is the function that will load the data from a fastjson.Value into an Item
// that the go-ap/activitypub package doesn't know about.
func JSONUnmarshalerFn(typ ap.Typer, val *fastjson.Value, i ap.Item) error {
	switch {
	case ap.ActivityVocabularyTypes{RepositoryType}.Match(typ):
		return OnRepository(i, func(r *Repository) error {
			return JSONLoadRepository(val, r)
		})
	case ap.ActivityVocabularyTypes{MergeRequestType}.Match(typ):
		return OnMergeRequest(i, func(m *ForgeMergeRequest) error {
			return JSONLoadMergeRequest(val, m)
		})
	default:
		return nil
	}
}

// NotEmpty is the function that checks if an object is empty
func NotEmpty(i ap.Item) bool {
	if ap.IsNil(i) {
		return false
	}
	switch {
	case ap.ActivityVocabularyTypes{RepositoryType}.Match(i.GetType()):
		r, err := ToRepository(i)
		if err != nil {
			return false
		}
		return ap.NotEmpty(r.Actor)
	case ap.ActivityVocabularyTypes{MergeRequestType}.Match(i.GetType()):
		m, err := ToMergeRequest(i)
		if err != nil {
			return false
		}
		// The generic ap.NotEmpty cannot unwrap the embedded Object, so check
		// it directly.
		return ap.NotEmpty(&m.Object)
	default:
		return ap.NotEmpty(i)
	}
}
