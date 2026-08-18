// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package integration

import (
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	apiv1 "forgejo.org/routers/api/v1"
	apiv1_permissions_tests "forgejo.org/routers/api/v1/permissions/tests"
	"forgejo.org/tests"
)

func TestAPIv1Permissions(t *testing.T) {
	defer test.MockVariableValue(&setting.Service.DefaultAllowCreateOrganization, true)()
	defer test.MockVariableValue(&setting.IsInTesting, true)()
	defer test.MockVariableValue(&setting.DisableGitHooks, false)()

	defer tests.PrepareTestEnv(t)()

	// because setting.IsInTesting == true, it will record the
	// middleware sequence of each route it builds
	apiv1.Routes()

	apiv1_permissions_tests.APIv1Permissions(t)
}
