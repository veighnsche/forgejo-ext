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
	const UnknownType OwnerType = 15

	cases := []struct {
		typ   OwnerType
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
	const UnknownType APIOwnerType = ""

	cases := []struct {
		typ   APIOwnerType
		valid bool
	}{
		{APIOwnerTypeIndividual, true},
		{APIOwnerTypeRepository, true},
		{APIOwnerTypeOrganization, true},
		{UnknownType, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}

func TestIsProjectTemplateTypeValid(t *testing.T) {
	const UnknownType APITemplateType = ""

	cases := []struct {
		typ   APITemplateType
		valid bool
	}{
		{APITemplateTypeNone, true},
		{APITemplateTypeBasicKanban, true},
		{APITemplateTypeBugTriage, true},
		{UnknownType, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}

func TestIsProjectCardTypeValid(t *testing.T) {
	const UnknownType APICardType = ""

	cases := []struct {
		typ   APICardType
		valid bool
	}{
		{APICardTypeTextOnly, true},
		{APICardTypeImagesAndText, true},
		{UnknownType, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}

func TestIsProjectStatusValid(t *testing.T) {
	const UnknownType APIStatus = ""

	cases := []struct {
		typ   APIStatus
		valid bool
	}{
		{APIStatusOpen, true},
		{APIStatusClosed, true},
		{UnknownType, false},
	}

	for _, v := range cases {
		valid, _ := val.IsValid(v.typ)
		assert.Equal(t, v.valid, valid)
	}
}
