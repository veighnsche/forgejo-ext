// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package project

import (
	"fmt"

	"forgejo.org/modules/validation"
)

// Model Types
type (
	TemplateType uint8
	// TemplateConfig is used to identify the template type of project that is being created
	TemplateConfig struct {
		TemplateType TemplateType
		Translation  string
	}
)

const (
	// TemplateTypeNone is a project template type that has no predefined columns
	TemplateTypeNone TemplateType = iota
	// TemplateTypeBasicKanban is a project template type that has basic predefined columns
	TemplateTypeBasicKanban
	// TemplateTypeBugTriage is a project template type that has predefined columns suited to hunting down bugs
	TemplateTypeBugTriage
)

func (tt TemplateType) Convert() APITemplateType {
	switch tt {
	case TemplateTypeBasicKanban:
		return APITemplateTypeBasicKanban
	case TemplateTypeBugTriage:
		return APITemplateTypeBugTriage
	default:
		return APITemplateTypeNone
	}
}

func (tt TemplateType) Validate() []string {
	var result []string
	types := []any{
		TemplateTypeBasicKanban,
		TemplateTypeBugTriage,
		TemplateTypeNone,
	}
	result = validation.ValidateOneOf(tt, types, "TemplateType")
	return result
}

func GetTemplateConfigs() []TemplateConfig {
	return []TemplateConfig{
		{TemplateTypeNone, "repo.projects.type.none"},
		{TemplateTypeBasicKanban, "repo.projects.type.basic_kanban"},
		{TemplateTypeBugTriage, "repo.projects.type.bug_triage"},
	}
}

type (
	CardType uint8
	// CardConfig is used to identify the type of column card that is being used
	CardConfig struct {
		CardType    CardType
		Translation string
	}
)

const (
	// CardTypeTextOnly is a project column card type that is text only
	CardTypeTextOnly CardType = iota
	// CardTypeImagesAndText is a project column card type that has images and text
	CardTypeImagesAndText
)

func (ct CardType) Convert() APICardType {
	switch ct {
	case CardTypeTextOnly:
		return APICardTypeTextOnly
	case CardTypeImagesAndText:
		return APICardTypeImagesAndText
	default:
		return APICardTypeTextOnly
	}
}

func (ct CardType) Validate() []string {
	var result []string
	types := []any{
		CardTypeTextOnly,
		CardTypeImagesAndText,
	}
	result = validation.ValidateOneOf(ct, types, "CardType")
	return result
}

// GetCardConfig retrieves the types of configurations project column cards could have
//
//llu:returnsTrKeyWeak
func GetCardConfig() []CardConfig {
	return []CardConfig{
		{CardTypeTextOnly, "repo.projects.card_type.text_only"},
		{CardTypeImagesAndText, "repo.projects.card_type.images_and_text"},
	}
}

type OwnerType uint8

const (
	// TypeIndividual is a type of project that is owned by an individual
	TypeIndividual OwnerType = iota + 1
	// TypeRepository is a project that is tied to a repository
	TypeRepository
	// TypeOrganization is a project that is tied to an organisation
	TypeOrganization
)

func (pt OwnerType) Convert() APIOwnerType {
	switch pt {
	case TypeIndividual:
		return APIOwnerTypeIndividual
	case TypeRepository:
		return APIOwnerTypeRepository
	default:
		return APIOwnerTypeOrganization
	}
}

func (pt OwnerType) Validate() []string {
	var result []string
	types := []any{
		TypeIndividual,
		TypeRepository,
		TypeOrganization,
	}
	result = validation.ValidateOneOf(pt, types, "ProjectType")
	return result
}

// API Types
type APIOwnerType string

const (
	APIOwnerTypeIndividual   APIOwnerType = "individual"
	APIOwnerTypeRepository   APIOwnerType = "repository"
	APIOwnerTypeOrganization APIOwnerType = "organization"
)

func (pt APIOwnerType) Convert() OwnerType {
	switch pt {
	case APIOwnerTypeIndividual:
		return TypeIndividual
	case APIOwnerTypeRepository:
		return TypeRepository
	default:
		return TypeOrganization
	}
}

func (pt APIOwnerType) Validate() []string {
	var result []string
	types := []any{
		APIOwnerTypeIndividual,
		APIOwnerTypeRepository,
		APIOwnerTypeOrganization,
	}
	result = validation.ValidateOneOf(pt, types, "ProjectAPIType")
	return result
}

func (pt APIOwnerType) String() string {
	return string(pt)
}

type APITemplateType string

const (
	APITemplateTypeNone        APITemplateType = "none"
	APITemplateTypeBasicKanban APITemplateType = "basic_kanban"
	APITemplateTypeBugTriage   APITemplateType = "bug_triage"
)

func (p APITemplateType) Convert() TemplateType {
	switch p {
	case APITemplateTypeBasicKanban:
		return TemplateTypeBasicKanban
	case APITemplateTypeBugTriage:
		return TemplateTypeBugTriage
	default:
		return TemplateTypeNone
	}
}

func (p APITemplateType) Validate() []string {
	var result []string
	types := []any{
		APITemplateTypeNone,
		APITemplateTypeBasicKanban,
		APITemplateTypeBugTriage,
	}
	result = validation.ValidateOneOf(p, types, "ProjectTemplateType")
	return result
}

func (p APITemplateType) String() string {
	return string(p)
}

type APICardType string

const (
	APICardTypeTextOnly      APICardType = "text_only"
	APICardTypeImagesAndText APICardType = "images_and_text"
)

func (p APICardType) Convert() CardType {
	switch p {
	case APICardTypeTextOnly:
		return CardTypeTextOnly
	case APICardTypeImagesAndText:
		return CardTypeImagesAndText
	default:
		return CardTypeTextOnly
	}
}

func (p APICardType) Validate() []string {
	var result []string
	types := []any{
		APICardTypeTextOnly,
		APICardTypeImagesAndText,
	}
	result = validation.ValidateOneOf(p, types, "ProjectCardType")
	return result
}

func (p APICardType) String() string {
	return string(p)
}

// APIStatus is the project status.
type APIStatus string

const (
	// APIStatusOpen is the project status of an open project.
	APIStatusOpen APIStatus = "open"
	// APIStatusClosed is the project status of a closed project.
	APIStatusClosed APIStatus = "closed"
)

// Validate checks if project status is valid.
func (p APIStatus) Validate() []string {
	var result []string
	types := []any{
		APIStatusOpen,
		APIStatusClosed,
	}
	result = validation.ValidateOneOf(p, types, "ProjectStatus")
	return result
}

func (p APIStatus) String() string {
	return string(p)
}

// IsClosed returns whether project status is closed.
func (p APIStatus) IsClosed() bool {
	return p == APIStatusClosed
}

func ProjectLinkForOrg(org string, projectID int64) string { //nolint
	return fmt.Sprintf("%s/-/projects/%d", org, projectID)
}

func ProjectLinkForRepo(repo string, projectID int64) string { //nolint
	return fmt.Sprintf("%s/projects/%d", repo, projectID)
}
