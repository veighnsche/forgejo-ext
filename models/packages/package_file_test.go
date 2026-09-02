// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPL-3.0-or-later
package packages

import (
	"testing"

	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
)

func TestCalculateFileSize(t *testing.T) {
	defer unittest.OverrideFixtures("models/packages/fixtures/TestCalculateFileSize")()
	require.NoError(t, unittest.PrepareTestDatabase())

	size, err := CalculateFileSize(t.Context(), &PackageFileSearchOptions{
		OwnerID: 1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(10), size)
}
