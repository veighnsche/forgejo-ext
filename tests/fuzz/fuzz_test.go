// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package fuzz

import (
	"bytes"
	"context"
	"io"
	"slices"
	"testing"

	"forgejo.org/modules/markup"
	"forgejo.org/modules/markup/markdown"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var renderContext = markup.RenderContext{
	Ctx: context.Background(),
	Links: markup.Links{
		Base: "https://example.com/go-gitea/gitea",
	},
	Metas: map[string]string{
		"user": "go-gitea",
		"repo": "gitea",
	},
}

func FuzzMarkdownRenderRaw(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		setting.IsInTesting = true
		setting.AppURL = "http://localhost:3000/"
		markdown.RenderRaw(&renderContext, bytes.NewReader(data), io.Discard)
	})
}

func FuzzMarkupPostProcess(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		setting.IsInTesting = true
		setting.AppURL = "http://localhost:3000/"
		markup.PostProcess(&renderContext, bytes.NewReader(data), io.Discard)
	})
}

func FuzzSignatureAlgorithms(f *testing.F) {
	algs := []setting.Algorithm{
		setting.AlgorithmEd25519,
		setting.AlgorithmHMACSHA256,
		setting.AlgorithmP256CAVAGE,
		setting.AlgorithmP256RFC9421,
		setting.AlgorithmP384CAVAGE,
		setting.AlgorithmP384RFC9421,
		setting.AlgorithmRSASHA512CAVAGE,
		setting.AlgorithmRSAPSSRFC9421,
		setting.AlgorithmRSASHA256CAVAGE,
		setting.AlgorithmRSARFC9421,
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		randAlg := string(data)

		if !slices.Contains(algs, setting.Algorithm(randAlg)) {
			ret, err := setting.AlgorithmFromString(randAlg)
			require.Error(t, err)
			assert.Equal(t, setting.AlgorithmNone, ret)
		}
	})
}
