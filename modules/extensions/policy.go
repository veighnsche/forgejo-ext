// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"errors"
	"sync/atomic"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/modules/setting"
)

var ErrRequiredPolicyUnavailable = errors.New("required extension policy unavailable")

// ErrPolicyDenied is a veto from an administrator-required extension.
type ErrPolicyDenied struct{ ReasonCode string }

func (e ErrPolicyDenied) Error() string {
	return "username rejected by required extension policy: " + e.ReasonCode
}

type PolicyEvaluator interface {
	EvaluateRequiredPolicy(context.Context, string, sdk.PolicyRequest) (sdk.PolicyDecision, error)
}
type policyRuntime struct{ evaluator PolicyEvaluator }

var requiredPolicyRuntime atomic.Pointer[policyRuntime]

// SetPolicyRuntime binds the shared model gate to the running extension manager.
func SetPolicyRuntime(evaluator PolicyEvaluator) {
	if evaluator == nil {
		requiredPolicyRuntime.Store(nil)
		return
	}
	requiredPolicyRuntime.Store(&policyRuntime{evaluator: evaluator})
}

// CheckRequiredPolicy is inactive only when no extension is configured as required.
// A configured requirement always fails closed until its runtime is ready.
func CheckRequiredPolicy(ctx context.Context, policyID string, request sdk.PolicyRequest) error {
	if len(setting.Extensions.RequiredIDs) == 0 {
		return nil
	}
	runtime := requiredPolicyRuntime.Load()
	if !setting.Extensions.Enabled || runtime == nil {
		return ErrRequiredPolicyUnavailable
	}
	decision, err := runtime.evaluator.EvaluateRequiredPolicy(ctx, policyID, request)
	if err != nil {
		return ErrRequiredPolicyUnavailable
	}
	if !decision.Allowed {
		return ErrPolicyDenied{ReasonCode: decision.ReasonCode}
	}
	return nil
}
