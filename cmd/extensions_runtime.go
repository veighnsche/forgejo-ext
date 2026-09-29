// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"

	web_extensions "forgejo.org/routers/web/extensions"
)

// startExtensionsForCLI shares web bootstrap and holds the package lock until the
// command finishes. An active web process therefore cannot be bypassed offline.
func startExtensionsForCLI(ctx context.Context) (func() error, error) {
	return web_extensions.StartRuntime(ctx)
}
