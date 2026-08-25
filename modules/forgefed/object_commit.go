// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed

import (
	ap "github.com/go-ap/activitypub"
	"github.com/valyala/fastjson"
)

const (
	// CommitType is the ForgeFed Commit object type.
	CommitType ap.ActivityVocabularyType = "Commit"
)

// ForgeCommit represents a single commit in the OrderedCollection carried by a
// ForgeFed Push activity. It adds the ForgeFed `hash` and `summary` properties
// on top of the standard ActivityPub Object.
type ForgeCommit struct {
	ap.Object
	// Hash the full SCM commit hash.
	Hash string `jsonld:"hash,omitempty"`
	// Summary a short human-readable summary of the commit.
	Summary string `jsonld:"summary,omitempty"`
}

// MarshalJSON writes the commit as an ActivityPub Object with the ForgeFed
// hash and summary properties appended.
func (c ForgeCommit) MarshalJSON() ([]byte, error) {
	b, err := c.Object.MarshalJSON()
	if len(b) == 0 || err != nil {
		return nil, err
	}

	b = b[:len(b)-1]
	if c.Hash != "" {
		ap.JSONWriteStringProp(&b, "hash", c.Hash)
	}
	if c.Summary != "" {
		ap.JSONWriteStringProp(&b, "summary", c.Summary)
	}
	ap.JSONWrite(&b, '}')
	return b, nil
}

func (c *ForgeCommit) UnmarshalJSON(data []byte) error {
	p := fastjson.Parser{}
	val, err := p.ParseBytes(data)
	if err != nil {
		return err
	}
	return JSONLoadForgeCommit(val, c)
}

func JSONLoadForgeCommit(val *fastjson.Value, c *ForgeCommit) error {
	if err := ap.OnObject(&c.Object, func(o *ap.Object) error {
		return ap.JSONLoadObject(val, o)
	}); err != nil {
		return err
	}
	c.Hash = string(val.GetStringBytes("hash"))
	c.Summary = string(val.GetStringBytes("summary"))
	return nil
}

func NewForgeCommit(id ap.IRI, hash, summary string) *ForgeCommit {
	c := &ForgeCommit{
		Object:  *ap.ObjectNew(ap.ObjectType),
		Hash:    hash,
		Summary: summary,
	}
	c.ID = id
	c.Type = CommitType
	return c
}
