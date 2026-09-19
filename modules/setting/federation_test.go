// Copyright 2025, 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var algs = []Algorithm{
	AlgorithmEd25519,
	AlgorithmHMACSHA256,
	AlgorithmP256CAVAGE,
	AlgorithmP256RFC9421,
	AlgorithmP384CAVAGE,
	AlgorithmP384RFC9421,
	AlgorithmRSASHA512CAVAGE,
	AlgorithmRSAPSSRFC9421,
	AlgorithmRSASHA256CAVAGE,
	AlgorithmRSARFC9421,
}

func TestSettingAlgorithm(t *testing.T) {
	for _, alg := range algs {
		ret, err := AlgorithmFromString(string(alg))
		require.NoError(t, err)
		assert.Equal(t, ret, alg)
	}
}
