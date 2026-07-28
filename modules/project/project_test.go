// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package project

import (
	"testing"

	val "forgejo.org/modules/validation"
	"github.com/stretchr/testify/assert"
)

func TestIsTemplateTypeValid(t *testing.T) {
	const TemplateTypeUnknown TemplateType = 15

	cases := []struct {
		typ   TemplateType
		valid bool
	}{
		{TemplateTypeNone, true},
		{TemplateTypeBasicKanban, true},
		{TemplateTypeBugTriage, true},
		{TemplateTypeUnknown, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}

func TestIsCardTypeValid(t *testing.T) {
	const UnknownType CardType = 15

	cases := []struct {
		typ   CardType
		valid bool
	}{
		{CardTypeTextOnly, true},
		{CardTypeImagesAndText, true},
		{UnknownType, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}

func TestIsProjectTypeValid(t *testing.T) {
	const UnknownType ProjectType = 15

	cases := []struct {
		typ   ProjectType
		valid bool
	}{
		{TypeIndividual, true},
		{TypeRepository, true},
		{TypeOrganization, true},
		{UnknownType, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}

func TestIsProjectAPITypeValid(t *testing.T) {
	const UnknownType ProjectAPIType = ""

	cases := []struct {
		typ   ProjectAPIType
		valid bool
	}{
		{ProjectAPITypeIndividual, true},
		{ProjectAPITypeRepository, true},
		{ProjectAPITypeOrganization, true},
		{UnknownType, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}

func TestIsProjectTemplateTypeValid(t *testing.T) {
	const UnknownType ProjectTemplateType = ""

	cases := []struct {
		typ   ProjectTemplateType
		valid bool
	}{
		{ProjectTemplateTypeNone, true},
		{ProjectTemplateTypeBasicKanban, true},
		{ProjectTemplateTypeBugTriage, true},
		{UnknownType, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}

func TestIsProjectCardTypeValid(t *testing.T) {
	const UnknownType ProjectCardType = ""

	cases := []struct {
		typ   ProjectCardType
		valid bool
	}{
		{ProjectCardTypeTextOnly, true},
		{ProjectCardTypeImagesAndText, true},
		{UnknownType, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}

func TestIsProjectStatusValid(t *testing.T) {
	const UnknownType ProjectStatus = ""

	cases := []struct {
		typ   ProjectStatus
		valid bool
	}{
		{StatusOpen, true},
		{StatusClosed, true},
		{UnknownType, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}
