// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package context

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"forgejo.org/modules/extensions"
	"github.com/stretchr/testify/require"
)

func TestRequiredPolicyErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{extensions.ErrPolicyDenied{ReasonCode: "reserved"}, http.StatusUnprocessableEntity},
		{fmt.Errorf("external authentication: %w", extensions.ErrRequiredPolicyUnavailable), http.StatusServiceUnavailable},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			for _, api := range []bool{false, true} {
				recorder := httptest.NewRecorder()
				base, close := NewBaseContext(recorder, httptest.NewRequest("POST", "/", nil))
				if api {
					ctx := &APIContext{Base: base}
					ctx.ServerError("create user", tc.err)
				} else {
					ctx := &Context{Base: base}
					ctx.ServerError("create user", tc.err)
				}
				close()
				require.Equal(t, tc.status, recorder.Code)
			}
		})
	}
	require.Zero(t, policyErrorStatus(fmt.Errorf("unrelated failure")))
}
