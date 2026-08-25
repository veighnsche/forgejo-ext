// Copyright 2023 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgefed

import (
	"net/url"

	"forgejo.org/modules/validation"

	"github.com/valyala/fastjson"
)

// ToDo: Search for full text SourceType and Source, also in .md files
type (
	SoftwareNameType string
)

const (
	ForgejoSourceType    SoftwareNameType = "forgejo"
	GiteaSourceType      SoftwareNameType = "gitea"
	MastodonSourceType   SoftwareNameType = "mastodon"
	GoToSocialSourceType SoftwareNameType = "gotosocial"
)

var KnownSourceTypes = []any{
	ForgejoSourceType, GiteaSourceType, MastodonSourceType, GoToSocialSourceType,
}

// ------------------------------------------------ NodeInfoWellKnown ------------------------------------------------

// NodeInfo data type
// swagger:model
type NodeInfoWellKnown struct {
	Href string
}

// Factory function for NodeInfoWellKnown. Created struct is asserted to be valid.
func NewNodeInfoWellKnown(body []byte) (NodeInfoWellKnown, error) {
	result, err := NodeInfoWellKnownUnmarshalJSON(body)
	if err != nil {
		return NodeInfoWellKnown{}, err
	}

	if valid, err := validation.IsValid(result); !valid {
		return NodeInfoWellKnown{}, err
	}

	return result, nil
}

func NodeInfoWellKnownUnmarshalJSON(data []byte) (NodeInfoWellKnown, error) {
	p := fastjson.Parser{}
	val, err := p.ParseBytes(data)
	if err != nil {
		return NodeInfoWellKnown{}, err
	}
	href := string(val.GetStringBytes("links", "0", "href"))
	return NodeInfoWellKnown{Href: href}, nil
}

// Validate collects error strings in a slice and returns this
func (node NodeInfoWellKnown) Validate() []string {
	var result []string
	result = append(result, validation.ValidateNotEmpty(node.Href, "Href")...)

	parsedURL, err := url.Parse(node.Href)
	if err != nil {
		result = append(result, err.Error())
		return result
	}

	if parsedURL.Host == "" {
		result = append(result, "Href has to be absolute")
	}

	result = append(result, validation.ValidateOneOf(parsedURL.Scheme, []any{"http", "https"}, "parsedURL.Scheme")...)

	if parsedURL.RawQuery != "" {
		result = append(result, "Href may not contain query")
	}

	return result
}

// ------------------------------------------------ NodeInfo ------------------------------------------------

// NodeInfo data type
// swagger:model
type NodeInfo struct {
	SoftwareName SoftwareNameType
	Version      string
}

func NodeInfoUnmarshalJSON(data []byte) (NodeInfo, error) {
	p := fastjson.Parser{}
	val, err := p.ParseBytes(data)
	if err != nil {
		return NodeInfo{}, err
	}
	source := string(val.GetStringBytes("software", "name"))
	version := string(val.GetStringBytes("software", "version"))
	result := NodeInfo{}
	result.SoftwareName = SoftwareNameType(source)
	result.Version = version
	return result, nil
}

func NewNodeInfo(body []byte) (NodeInfo, error) {
	result, err := NodeInfoUnmarshalJSON(body)
	if err != nil {
		return NodeInfo{}, err
	}

	if valid, err := validation.IsValid(result); !valid {
		return NodeInfo{}, err
	}
	return result, nil
}

// Validate collects error strings in a slice and returns this
func (node NodeInfo) Validate() []string {
	var result []string
	result = append(result, validation.ValidateNotEmpty(string(node.SoftwareName), "node.SoftwareName")...)
	result = append(result, validation.ValidateOneOf(node.SoftwareName, KnownSourceTypes, "node.SoftwareName")...)

	return result
}

// IsForge returns true when the remote instance is a forge speaking the
// ForgeFed vocabulary (Forgejo or Gitea) and can therefore process
// repository-level activities such as Like (star) or repository follows.
func (node NodeInfo) IsForge() bool {
	return node.SoftwareName == ForgejoSourceType || node.SoftwareName == GiteaSourceType
}

// IsForgejo returns true when the remote instance is a Forgejo instance.
func (node NodeInfo) IsForgejo() bool {
	return node.SoftwareName == ForgejoSourceType
}

// SupportsRepositoryActivities reports whether repository-scoped ForgeFed
// activities should be sent to the remote host. Mastodon, GoToSocial and
// other microblogging servers do not understand the ForgeFed vocabulary, so
// repository-level activities are only emitted for forge peers. Person-level
// activities (follows, notes) remain interoperable across the whole fediverse.
func (node NodeInfo) SupportsRepositoryActivities() bool {
	return node.IsForge()
}
