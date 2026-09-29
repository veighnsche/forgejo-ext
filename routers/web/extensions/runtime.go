// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"errors"

	"forgejo.org/modules/setting"
	runtime "forgejo.org/services/extensions"
)

// StartRuntime loads the administrator-selected packages for web, CLI and installer
// identity operations. The caller keeps the package lock until it invokes close.
func StartRuntime(ctx context.Context) (close func() error, err error) {
	if !setting.Extensions.Enabled {
		if len(setting.Extensions.RequiredIDs) != 0 {
			return nil, errors.New("required extensions are configured but extensions are disabled")
		}
		return func() error { return nil }, nil
	}
	manager := runtime.NewManager(setting.Extensions.Path, setting.Extensions.RequiredIDs...)
	if err := manager.SetCallbackHandlerFactory(CallbackHandlerForInstance); err != nil {
		return nil, err
	}
	if err := manager.SetServiceCallbackEndpoint(setting.Extensions.ServiceCallbackPath, CallbackHandlerForService()); err != nil {
		return nil, err
	}
	if err := manager.SetInstanceStopped(RevokeAdmissionsForInstance); err != nil {
		return nil, err
	}
	if err := manager.Start(ctx); err != nil {
		return nil, err
	}
	runtime.SetDefault(manager)
	return func() error {
		runtime.SetDefault(nil)
		return manager.Close()
	}, nil
}
