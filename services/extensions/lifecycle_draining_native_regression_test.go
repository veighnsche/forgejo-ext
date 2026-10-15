// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"testing"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"
)

func TestObserveCompletedDrainFailureIsUnconfirmed(t *testing.T) {
	failure := &packages.LifecycleError{Code: packages.LifecycleErrUnconfirmed, Message: "external cleanup remains unknown"}
	for _, last := range []*packages.LifecycleError{failure, nil} {
		snapshot := observeSnapshot{started: true, draining: true, backendAlive: true}
		got := observeBaseResult("", "tailnet", snapshot, packages.Intent{}, nil, last)
		if got.State != packages.LifecycleFailed || !got.Unconfirmed || got.Error == nil {
			t.Fatalf("completed drain failure hidden: %+v", got)
		}
		if last != nil && got.Error != last {
			t.Fatal("last failure lost")
		}
		snapshot.busy = true
		snapshot.busyOp = sdk.LifecycleDeactivate
		live := observeBaseResult("", "tailnet", snapshot, packages.Intent{}, nil, last)
		if live.State != packages.LifecycleDeactivating || live.Unconfirmed || live.Error != nil {
			t.Fatalf("live operation no longer reports progress: %+v", live)
		}
	}
}
