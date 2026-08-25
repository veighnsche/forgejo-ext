// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed

import (
	ap "github.com/go-ap/activitypub"
	"github.com/valyala/fastjson"
)

// ForgePush represents a ForgeFed Push activity: new commits have been added
// to a Repository (https://forgefed.org/ns#Push). It is published by the
// Repository actor and carries the pushed Commit objects in an
// OrderedCollection, together with the SCM hashes before and after the push.
type ForgePush struct {
	ap.Activity
	// HashBefore the SCM hash immediately before the push.
	HashBefore string `jsonld:"hashBefore,omitempty"`
	// HashAfter the SCM hash immediately after the push.
	HashAfter string `jsonld:"hashAfter,omitempty"`
}

// MarshalJSON writes the activity with the ForgeFed hashBefore and hashAfter
// properties appended.
func (p ForgePush) MarshalJSON() ([]byte, error) {
	b, err := p.Activity.MarshalJSON()
	if len(b) == 0 || err != nil {
		return nil, err
	}

	b = b[:len(b)-1]
	if p.HashBefore != "" {
		ap.JSONWriteStringProp(&b, "hashBefore", p.HashBefore)
	}
	if p.HashAfter != "" {
		ap.JSONWriteStringProp(&b, "hashAfter", p.HashAfter)
	}
	ap.JSONWrite(&b, '}')
	return b, nil
}

func (p *ForgePush) UnmarshalJSON(data []byte) error {
	raw := fastjson.Parser{}
	val, err := raw.ParseBytes(data)
	if err != nil {
		return err
	}
	return JSONLoadForgePush(val, p)
}

func JSONLoadForgePush(val *fastjson.Value, p *ForgePush) error {
	if err := ap.OnActivity(&p.Activity, func(a *ap.Activity) error {
		return ap.JSONLoadActivity(val, a)
	}); err != nil {
		return err
	}
	p.HashBefore = string(val.GetStringBytes("hashBefore"))
	p.HashAfter = string(val.GetStringBytes("hashAfter"))
	return nil
}

// NewForgePush creates a ForgeFed Push activity published by the given
// repository actor.
func NewForgePush(id, actor ap.IRI) *ForgePush {
	p := &ForgePush{
		Activity: *ap.ActivityNew(id, PushType, nil),
	}
	p.Type = PushType
	p.Actor = actor
	return p
}
