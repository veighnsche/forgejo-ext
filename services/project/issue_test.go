// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package project

import (
	"errors"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetValidProjectIssue(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	validProjectID := int64(1)
	nonExistingProjectID := int64(99999)

	validColumnID := int64(1)
	differentColID := int64(4)

	validProjectIssueID := int64(1)
	nonExistingProjectIssueID := int64(99999)
	differentProjectIssueID := int64(4)

	t.Run("GetValidProjectIssue", func(t *testing.T) {
		_, err := GetValidProjectIssueByID(t.Context(), validProjectID, validColumnID, validProjectIssueID)
		require.NoError(t, err)

		_, err = GetValidProjectIssueByID(t.Context(), validProjectID, differentColID, 0)
		assert.Contains(t, err.Error(), invalidStr)

		_, err = GetValidProjectIssueByID(t.Context(), validProjectID, validColumnID, nonExistingProjectIssueID)
		assert.Contains(t, err.Error(), notExistStr)

		_, err = GetValidProjectIssueByID(t.Context(), validProjectID, validColumnID, differentProjectIssueID)
		assert.True(t, errors.Is(err, util.ErrInvalidArgument))

		_, err = GetValidProjectColumnByID(t.Context(), nonExistingProjectID, validColumnID)
		assert.True(t, errors.Is(err, util.ErrInvalidArgument))
	})
}
