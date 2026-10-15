// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package integration

import (
	"net/url"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	apiv1 "forgejo.org/routers/api/v1"
	apiv1_permissions_tests "forgejo.org/routers/api/v1/permissions/tests"
)

func TestAPIv1Permissions(t *testing.T) {
	defer test.MockVariableValue(&setting.Service.DefaultAllowCreateOrganization, true)()
	defer test.MockVariableValue(&setting.IsInTesting, true)()
	defer test.MockVariableValue(&setting.DisableGitHooks, false)()

	// PR fixtures perform real ref writes; their hooks verify ownership
	// through the application's private callback server.
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		// Record the middleware sequence of each route while testing.
		apiv1.Routes()
		apiv1_permissions_tests.APIv1Permissions(t)
	})
}
