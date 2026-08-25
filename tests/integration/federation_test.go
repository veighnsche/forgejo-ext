// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"testing"

	"forgejo.org/modules/hostmatcher"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
)

// mockFederationAllowAllHosts disables the federation host policy for the
// duration of the test: every host is allowed. Federation integration tests
// talk to mock federation servers on ephemeral loopback ports, which the
// default opt-in (empty allowlist) policy would reject.
func mockFederationAllowAllHosts(t *testing.T) {
	t.Helper()
	reset := test.MockVariableValue(&setting.FederationAllowedHostList, hostmatcher.ParseHostMatchList("", "*"))
	t.Cleanup(reset)
}
