// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"testing"
	"time"

	model "forgejo.org/models/nativeoperation"

	"github.com/stretchr/testify/require"
)

func TestParseLifecycleReviewPayloadAcceptsExactInput(t *testing.T) {
	intent, err := parseLifecycleReviewPayload([]byte(`{"issue_id":88,"reviewer_ids":[15,23]}`), 7)
	require.NoError(t, err)
	require.Equal(t, int64(88), intent.IssueID)
	require.Equal(t, []int64{15, 23}, intent.ReviewerIDs)
}

func TestParseLifecycleReviewPayloadRejects(t *testing.T) {
	cases := map[string]string{
		"empty":      ``,
		"unknown":    `{"issue_id":88,"reviewer_ids":[15],"team_ids":[3]}`,
		"no issue":   `{"reviewer_ids":[15]}`,
		"no reviews": `{"issue_id":88}`,
		"empty list": `{"issue_id":88,"reviewer_ids":[]}`,
		"too many":   `{"issue_id":88,"reviewer_ids":[1,2,3,4]}`,
		"duplicate":  `{"issue_id":88,"reviewer_ids":[15,15]}`,
		"zero id":    `{"issue_id":88,"reviewer_ids":[0]}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseLifecycleReviewPayload([]byte(payload), 7)
			require.ErrorIs(t, err, ErrInvalidIntent)
		})
	}
}

func TestValidateIntentAcceptsLifecycleReview(t *testing.T) {
	now := time.Now().Unix()
	intent, err := ValidateIntent("op-lifecycle-r1", 2, 1, model.KindLifecycleReview, "rev-1", 1, now+300, []byte(`{"issue_id":88,"reviewer_ids":[15]}`), now)
	require.NoError(t, err)
	require.NotNil(t, intent.LifecycleReview)
	require.NotEmpty(t, intent.Digest)

	_, err = ValidateIntent("op-lifecycle-r2", 2, 1, model.KindLifecycleReview, "rev-1", 1, now+300, []byte(`{"issue_id":88,"reviewer_ids":[1,2,3,4]}`), now)
	require.ErrorIs(t, err, ErrInvalidIntent)
}
