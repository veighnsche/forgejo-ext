// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"errors"
	"testing"

	"forgejo.org/models"
	model "forgejo.org/models/nativeoperation"
	"forgejo.org/modules/git"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
)

func TestMapExactMergeRefusal(t *testing.T) {
	cases := map[string]string{
		pull_service.ExactMergeRefusedStaleHead:      model.ReasonStaleHead,
		pull_service.ExactMergeRefusedMissingHead:    model.ReasonStaleHead,
		pull_service.ExactMergeRefusedStaleBase:      model.ReasonStaleBaseOrResult,
		pull_service.ExactMergeRefusedMissingBase:    model.ReasonStaleBaseOrResult,
		pull_service.ExactMergeRefusedNoOp:           model.ReasonStaleBaseOrResult,
		pull_service.ExactMergeRefusedCrossRepo:      model.ReasonPRMismatch,
		pull_service.ExactMergeRefusedPRMismatch:     model.ReasonPRMismatch,
		pull_service.ExactMergeRefusedClosedOrMerged: model.ReasonPRMismatch,
		// Unreachable from a validated intent, but closed when seen.
		pull_service.ExactMergeRefusedMalformedRef:     model.ReasonNativeRefused,
		pull_service.ExactMergeRefusedMalformedOID:     model.ReasonNativeRefused,
		pull_service.ExactMergeRefusedMethodNotAllowed: model.ReasonNativeRefused,
	}
	for reason, want := range cases {
		assert.Equal(t, want, mapExactMergeRefusal(pull_service.ErrExactMergeRefused{Reason: reason}), reason)
	}
	assert.Equal(t, model.ReasonNativeRefused, mapExactMergeRefusal(pull_service.ErrExactMergeRefused{Reason: "future reason"}))
	assert.Equal(t, model.ReasonNativeRefused, mapExactMergeRefusal(errors.New("boom")))
}

func TestMapMergeEngineError(t *testing.T) {
	assert.Equal(t, model.ReasonNotFastForward, mapMergeEngineError(models.ErrMergeDivergingFastForwardOnly{}))
	assert.Equal(t, model.ReasonNotFastForward, mapMergeEngineError(&git.ErrPushOutOfDate{}))
	assert.Equal(t, model.ReasonStaleHead, mapMergeEngineError(models.ErrSHADoesNotMatch{}))
	assert.Equal(t, model.ReasonNativeRefused, mapMergeEngineError(models.ErrDisallowedToMerge{}))
	assert.Equal(t, model.ReasonNativeRefused, mapMergeEngineError(pull_service.ErrNotMergeableState))
	assert.Equal(t, model.ReasonNativeRefused, mapMergeEngineError(pull_service.ErrIsChecking))
	assert.Equal(t, model.ReasonNativeRefused, mapMergeEngineError(errors.New("boom")))
}
