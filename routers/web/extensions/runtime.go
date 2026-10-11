// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"errors"
	"fmt"
	user_model "forgejo.org/models/user"
	"net/http"

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
	peers, err := runtime.ParseServiceBridgePeers(setting.Extensions.ServiceBridgePeers)
	if err != nil {
		return nil, err
	}
	if err := manager.SetServiceBridgePeers(peers); err != nil {
		return nil, err
	}
	if err := manager.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return BackgroundMux(manager, CallbackHandlerForInstance(instanceID), instanceID, false)
	}); err != nil {
		return nil, err
	}
	if err := manager.SetServiceCallbackEndpoint(setting.Extensions.ServiceCallbackPath, BackgroundMux(manager, CallbackHandlerForService(), "", true)); err != nil {
		return nil, err
	}
	if err := manager.SetInstanceStopped(RevokeAdmissionsForInstance); err != nil {
		return nil, err
	}
	if err := manager.Start(ctx); err != nil {
		return nil, err
	}
	for _, descriptor := range manager.List() {
		for _, page := range descriptor.Manifest.Pages {
			if page.Scope != "global" || page.Route == "" {
				continue
			}
			_, err := user_model.GetUserByName(ctx, page.Route)
			if !user_model.IsErrUserNotExist(err) {
				_ = manager.Close()
				return nil, fmt.Errorf("native page route %q cannot claim an existing or unavailable account", page.Route)
			}
		}
	}
	runtime.SetDefault(manager)
	return func() error {
		runtime.SetDefault(nil)
		return manager.Close()
	}, nil
}
