// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed

import (
	"strings"

	"forgejo.org/modules/validation"

	ap "github.com/go-ap/activitypub"
	"github.com/valyala/fastjson"
)

// MergeRequestType is the ForgeFed type for a proposed code change.
const MergeRequestType ap.ActivityVocabularyType = "MergeRequest"

// ForgeMergeRequest represents a ForgeFed MergeRequest object: a proposal to
// merge the changes of a source branch (on a source repository) into a target
// branch of a target repository. It is offered to the target repository's
// inbox inside an Offer activity.
//
// swagger:model
type ForgeMergeRequest struct {
	// swagger.ignore
	ap.Object
	// Source is the actor IRI of the repository the change comes from.
	Source ap.IRI `jsonld:"source,omitempty"`
	// SourceGitURL is the git URL of the source repository, used by the
	// target instance to fetch the proposed branch.
	SourceGitURL string `jsonld:"sourceGitURL,omitempty"`
	// SourceBranch is the branch on the source repository containing the
	// proposed changes.
	SourceBranch string `jsonld:"sourceBranch,omitempty"`
	// Ref is the target branch on the target repository the change proposes
	// to merge into.
	Ref string `jsonld:"ref,omitempty"`
}

// NewForgeMergeRequest builds a ForgeMergeRequest. The created struct is
// asserted to be valid.
func NewForgeMergeRequest(id, source ap.IRI, sourceGitURL, sourceBranch, ref, title, content string) (ForgeMergeRequest, error) {
	result := ForgeMergeRequest{}
	result.Type = MergeRequestType
	result.ID = id
	result.Source = source
	result.SourceGitURL = sourceGitURL
	result.SourceBranch = sourceBranch
	result.Ref = ref
	if title != "" {
		result.Name = ap.NaturalLanguageValuesNew()
		if err := result.Name.Set(ap.NilLangRef, ap.Content(title)); err != nil {
			return result, err
		}
	}
	if content != "" {
		result.Content = ap.NaturalLanguageValuesNew()
		if err := result.Content.Set(ap.NilLangRef, ap.Content(content)); err != nil {
			return result, err
		}
	}
	if valid, err := validation.IsValid(result); !valid {
		return ForgeMergeRequest{}, err
	}
	return result, nil
}

func (m ForgeMergeRequest) MarshalJSON() ([]byte, error) {
	b, err := m.Object.MarshalJSON()
	if err != nil || len(b) == 0 {
		return nil, err
	}
	b = b[:len(b)-1]
	if m.Source != "" {
		ap.JSONWriteItemProp(&b, "source", m.Source)
	}
	if m.SourceGitURL != "" {
		ap.JSONWriteStringProp(&b, "sourceGitURL", m.SourceGitURL)
	}
	if m.SourceBranch != "" {
		ap.JSONWriteStringProp(&b, "sourceBranch", m.SourceBranch)
	}
	if m.Ref != "" {
		ap.JSONWriteStringProp(&b, "ref", m.Ref)
	}
	ap.JSONWrite(&b, '}')
	return b, nil
}

// JSONLoadMergeRequest loads a ForgeMergeRequest from a fastjson value,
// preserving the custom ForgeFed fields the generic parser drops.
func JSONLoadMergeRequest(val *fastjson.Value, m *ForgeMergeRequest) error {
	if err := ap.OnObject(&m.Object, func(o *ap.Object) error {
		return ap.JSONLoadObject(val, o)
	}); err != nil {
		return err
	}
	if source := val.GetStringBytes("source"); len(source) > 0 {
		m.Source = ap.IRI(source)
	}
	m.SourceGitURL = string(val.GetStringBytes("sourceGitURL"))
	m.SourceBranch = string(val.GetStringBytes("sourceBranch"))
	m.Ref = string(val.GetStringBytes("ref"))
	return nil
}

func (m *ForgeMergeRequest) UnmarshalJSON(data []byte) error {
	p := fastjson.Parser{}
	val, err := p.ParseBytes(data)
	if err != nil {
		return err
	}
	return JSONLoadMergeRequest(val, m)
}

func (m ForgeMergeRequest) Validate() []string {
	var result []string
	typeStr := ""
	if m.Type != nil {
		typeStr = m.Type.AsTypes().String()
	}
	result = append(result, validation.ValidateNotEmpty(typeStr, "type")...)
	result = append(result, validation.ValidateOneOf(typeStr, []any{string(MergeRequestType)}, "type")...)
	result = append(result, validation.ValidateNotEmpty(m.ID.String(), "id")...)
	result = append(result, validation.ValidateNotEmpty(m.Source.String(), "source")...)
	result = append(result, validation.ValidateNotEmpty(m.SourceGitURL, "sourceGitURL")...)
	result = append(result, validation.ValidateNotEmpty(m.SourceBranch, "sourceBranch")...)
	result = append(result, validation.ValidateNotEmpty(m.Ref, "ref")...)
	if strings.HasPrefix(m.SourceGitURL, "file://") {
		result = append(result, "sourceGitURL may not use the file scheme")
	}
	return result
}

// ToMergeRequest tries to convert the Item to a ForgeMergeRequest.
func ToMergeRequest(it ap.Item) (*ForgeMergeRequest, error) {
	switch i := it.(type) {
	case *ForgeMergeRequest:
		return i, nil
	case ForgeMergeRequest:
		return &i, nil
	default:
		return nil, ap.ErrorInvalidType[ap.Object](it)
	}
}

type withMergeRequestFn func(*ForgeMergeRequest) error

// OnMergeRequest calls function fn on the Item if it can be asserted to type
// *ForgeMergeRequest.
func OnMergeRequest(it ap.Item, fn withMergeRequestFn) error {
	if it == nil {
		return nil
	}
	ob, err := ToMergeRequest(it)
	if err != nil {
		return err
	}
	return fn(ob)
}
