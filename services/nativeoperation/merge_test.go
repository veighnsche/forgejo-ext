// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"errors"
	"testing"

	"forgejo.org/models"
	nativeoperation_model "forgejo.org/models/nativeoperation"
	"forgejo.org/modules/git"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
)

func TestMapExactMergeRefusal(t *testing.T) {
	cases := map[string]string{
		pull_service.ExactMergeRefusedStaleHead:      nativeoperation_model.ReasonStaleHead,
		pull_service.ExactMergeRefusedMissingHead:    nativeoperation_model.ReasonStaleHead,
		pull_service.ExactMergeRefusedStaleBase:      nativeoperation_model.ReasonStaleBaseOrResult,
		pull_service.ExactMergeRefusedMissingBase:    nativeoperation_model.ReasonStaleBaseOrResult,
		pull_service.ExactMergeRefusedNoOp:           nativeoperation_model.ReasonStaleBaseOrResult,
		pull_service.ExactMergeRefusedCrossRepo:      nativeoperation_model.ReasonPRMismatch,
		pull_service.ExactMergeRefusedPRMismatch:     nativeoperation_model.ReasonPRMismatch,
		pull_service.ExactMergeRefusedClosedOrMerged: nativeoperation_model.ReasonPRMismatch,
		// Unreachable from a validated intent, but closed when seen.
		pull_service.ExactMergeRefusedMalformedRef:     nativeoperation_model.ReasonNativeRefused,
		pull_service.ExactMergeRefusedMalformedOID:     nativeoperation_model.ReasonNativeRefused,
		pull_service.ExactMergeRefusedMethodNotAllowed: nativeoperation_model.ReasonNativeRefused,
	}
	for reason, want := range cases {
		assert.Equal(t, want, mapExactMergeRefusal(pull_service.ErrExactMergeRefused{Reason: reason}), reason)
	}
	assert.Equal(t, nativeoperation_model.ReasonNativeRefused, mapExactMergeRefusal(pull_service.ErrExactMergeRefused{Reason: "future reason"}))
	assert.Equal(t, nativeoperation_model.ReasonNativeRefused, mapExactMergeRefusal(errors.New("boom")))
}

func TestMapMergeEngineError(t *testing.T) {
	assert.Equal(t, nativeoperation_model.ReasonNotFastForward, mapMergeEngineError(models.ErrMergeDivergingFastForwardOnly{}))
	assert.Equal(t, nativeoperation_model.ReasonNotFastForward, mapMergeEngineError(&git.ErrPushOutOfDate{}))
	assert.Equal(t, nativeoperation_model.ReasonStaleHead, mapMergeEngineError(models.ErrSHADoesNotMatch{}))
	assert.Equal(t, nativeoperation_model.ReasonNativeRefused, mapMergeEngineError(models.ErrDisallowedToMerge{}))
	assert.Equal(t, nativeoperation_model.ReasonNativeRefused, mapMergeEngineError(pull_service.ErrNotMergeableState))
	assert.Equal(t, nativeoperation_model.ReasonNativeRefused, mapMergeEngineError(pull_service.ErrIsChecking))
	assert.Equal(t, nativeoperation_model.ReasonNativeRefused, mapMergeEngineError(errors.New("boom")))
}
