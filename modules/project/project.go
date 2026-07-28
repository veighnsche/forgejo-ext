// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package project

import (
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

func (tt TemplateType) Convert() ProjectTemplateType {
	switch tt {
	case TemplateTypeBasicKanban:
		return ProjectTemplateTypeBasicKanban
	case TemplateTypeBugTriage:
		return ProjectTemplateTypeBugTriage
	default:
		return ProjectTemplateTypeNone
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

func (ct CardType) Convert() ProjectCardType {
	switch ct {
	case CardTypeTextOnly:
		return ProjectCardTypeTextOnly
	case CardTypeImagesAndText:
		return ProjectCardTypeImagesAndText
	default:
		return ProjectCardTypeTextOnly
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
//llu:returnsTrKey
func GetCardConfig() []CardConfig {
	return []CardConfig{
		{CardTypeTextOnly, "repo.projects.card_type.text_only"},
		{CardTypeImagesAndText, "repo.projects.card_type.images_and_text"},
	}
}

type ProjectType uint8

const (
	// TypeIndividual is a type of project that is owned by an individual
	TypeIndividual ProjectType = iota + 1
	// TypeRepository is a project that is tied to a repository
	TypeRepository
	// TypeOrganization is a project that is tied to an organisation
	TypeOrganization
)

func (pt ProjectType) Convert() ProjectAPIType {
	switch pt {
	case TypeIndividual:
		return ProjectAPITypeIndividual
	case TypeRepository:
		return ProjectAPITypeRepository
	default:
		return ProjectAPITypeOrganization
	}
}

func (pt ProjectType) Validate() []string {
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
type ProjectAPIType string

const (
	ProjectAPITypeIndividual   ProjectAPIType = "individual"
	ProjectAPITypeRepository   ProjectAPIType = "repository"
	ProjectAPITypeOrganization ProjectAPIType = "organization"
)

func (pt ProjectAPIType) Convert() ProjectType {
	switch pt {
	case ProjectAPITypeIndividual:
		return TypeIndividual
	case ProjectAPITypeRepository:
		return TypeRepository
	default:
		return TypeOrganization
	}
}

func (pt ProjectAPIType) Validate() []string {
	var result []string
	types := []any{
		ProjectAPITypeIndividual,
		ProjectAPITypeRepository,
		ProjectAPITypeOrganization,
	}
	result = validation.ValidateOneOf(pt, types, "ProjectAPIType")
	return result
}

func (pt ProjectAPIType) String() string {
	return string(pt)
}

type ProjectTemplateType string

const (
	ProjectTemplateTypeNone        ProjectTemplateType = "none"
	ProjectTemplateTypeBasicKanban ProjectTemplateType = "basic_kanban"
	ProjectTemplateTypeBugTriage   ProjectTemplateType = "bug_triage"
)

func (p ProjectTemplateType) Convert() TemplateType {
	switch p {
	case ProjectTemplateTypeBasicKanban:
		return TemplateTypeBasicKanban
	case ProjectTemplateTypeBugTriage:
		return TemplateTypeBugTriage
	default:
		return TemplateTypeNone
	}
}

func (p ProjectTemplateType) Validate() []string {
	var result []string
	types := []any{
		ProjectTemplateTypeNone,
		ProjectTemplateTypeBasicKanban,
		ProjectTemplateTypeBugTriage,
	}
	result = validation.ValidateOneOf(p, types, "ProjectTemplateType")
	return result
}

func (p ProjectTemplateType) String() string {
	return string(p)
}

type ProjectCardType string

const (
	ProjectCardTypeTextOnly      ProjectCardType = "text_only"
	ProjectCardTypeImagesAndText ProjectCardType = "images_and_text"
)

func (p ProjectCardType) Convert() CardType {
	switch p {
	case ProjectCardTypeTextOnly:
		return CardTypeTextOnly
	case ProjectCardTypeImagesAndText:
		return CardTypeImagesAndText
	default:
		return CardTypeTextOnly
	}
}

func (p ProjectCardType) Validate() []string {
	var result []string
	types := []any{
		ProjectCardTypeTextOnly,
		ProjectCardTypeImagesAndText,
	}
	result = validation.ValidateOneOf(p, types, "ProjectCardType")
	return result
}

func (p ProjectCardType) String() string {
	return string(p)
}

// ProjectStatus is the project status.
type ProjectStatus string

const (
	// StatusOpen is the project status of an open project.
	StatusOpen ProjectStatus = "open"
	// StatusClosed is the project status of a closed project.
	StatusClosed ProjectStatus = "closed"
)

// Validate checks if project status is valid.
func (p ProjectStatus) Validate() []string {
	var result []string
	types := []any{
		StatusOpen,
		StatusClosed,
	}
	result = validation.ValidateOneOf(p, types, "ProjectStatus")
	return result
}

func (p ProjectStatus) String() string {
	return string(p)
}

// IsClosed returns whether project status is closed.
func (p ProjectStatus) IsClosed() bool {
	return p == StatusClosed
}
