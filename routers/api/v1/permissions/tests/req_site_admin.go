// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests

import (
	"testing"

	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.ReqSiteAdmin, functionTest{
	testCases: []*testCase{
		{
			data: newTestData(map[string]string{
				"doer": "doeradmin",
			}),
		},
		{
			data: newTestData(map[string]string{
				"doer": "regularuser",
			}),
			error: "user should be the site admin",
		},
	},
	fulfillNeeds: func(t *testing.T, data *testData) {
		t.Helper()
		// An earlier permission must succeed before the permission under
		// test runs, even when that later fixture supplies a regular user.
		// ReqSiteAdmin's own non-admin fixture does not call this helper.
		data.Set("doer", "doeradmin")
	},
})
