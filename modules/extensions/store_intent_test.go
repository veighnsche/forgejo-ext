// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

//go:fix inline

func ensureTestInstallation(t *testing.T, root, id string) string {
	t.Helper()
	installation, err := EnsureInstallation(root, id)
	require.NoError(t, err)
	return installation
}

func TestIntentRoundTripCAS(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "tailnet")

	absent, err := LoadIntent(root, "tailnet")
	require.NoError(t, err)
	require.Equal(t, "0", absent.Generation)
	require.False(t, absent.DesiredEnabled)
	require.Empty(t, absent.InstallationID)
	require.Nil(t, absent.PendingOperation)

	first, err := StoreIntent(root, "tailnet", installation, true, nil, "0")
	require.NoError(t, err)
	require.Equal(t, "1", first.Generation)
	require.True(t, first.DesiredEnabled)

	loaded, err := LoadIntent(root, "tailnet")
	require.NoError(t, err)
	require.Equal(t, first, loaded)

	second, err := StoreIntent(root, "tailnet", installation, false, new(IntentDeactivate), "1")
	require.NoError(t, err)
	require.Equal(t, "2", second.Generation)
	require.False(t, second.DesiredEnabled)
	require.Equal(t, IntentDeactivate, *second.PendingOperation)

	// A stale expected generation is a conflict, never another effect.
	_, err = StoreIntent(root, "tailnet", installation, true, nil, "1")
	require.ErrorContains(t, err, "changed")
	unchanged, err := LoadIntent(root, "tailnet")
	require.NoError(t, err)
	require.Equal(t, second, unchanged)
}

func TestIntentRejectsMalformed(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "tailnet")

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".intents"), 0o700))
	require.NoError(t, os.WriteFile(IntentPath(root, "tailnet"), []byte("private-token"), 0o600))
	_, err := LoadIntent(root, "tailnet")
	require.ErrorContains(t, err, "invalid")

	_, err = LoadIntent(root, "../escape")
	require.Error(t, err)

	// Unknown fields are rejected.
	require.NoError(t, os.WriteFile(IntentPath(root, "tailnet"), []byte(`{"schema":1,"package_id":"tailnet","installation_id":"`+installation+`","generation":"1","desired_enabled":true,"pending_operation":null,"extra":true}`), 0o600))
	_, err = LoadIntent(root, "tailnet")
	require.ErrorContains(t, err, "invalid")

	// Oversize documents are rejected.
	require.NoError(t, os.WriteFile(IntentPath(root, "tailnet"), []byte(`{"schema":1,"package_id":"tailnet","installation_id":"`+installation+`","generation":"1","desired_enabled":true,"pending_operation":null,"padding":"`+strings.Repeat("x", maximumIntentBytes)+`"}`), 0o600))
	_, err = LoadIntent(root, "tailnet")
	require.Error(t, err)
	require.NoError(t, os.Remove(IntentPath(root, "tailnet")))

	// Non-canonical and out-of-range generations are rejected.
	for _, generation := range []string{"", "00", "01", "-1", "1 ", "18446744073709551616"} {
		_, err = StoreIntent(root, "tailnet", installation, true, nil, generation)
		require.Error(t, err, "generation %q", generation)
	}

	// Pending operations must agree with the desired boolean.
	_, err = StoreIntent(root, "tailnet", installation, false, new(IntentActivate), "0")
	require.ErrorContains(t, err, "requires desired enabled")
	_, err = StoreIntent(root, "tailnet", installation, true, new(IntentDeactivate), "0")
	require.ErrorContains(t, err, "requires desired disabled")
	_, err = StoreIntent(root, "tailnet", installation, true, new("restart"), "0")
	require.ErrorContains(t, err, "operation is invalid")

	// The installation UUID must parse and match the recorded identity.
	_, err = StoreIntent(root, "tailnet", "not-a-uuid", true, nil, "0")
	require.ErrorContains(t, err, "installation is invalid")
	_, err = StoreIntent(root, "tailnet", "550e8400-e29b-41d4-a716-446655440000", true, nil, "0")
	require.ErrorContains(t, err, "does not match")
}

func TestIntentRemoveIsIdempotent(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "tailnet")
	_, err := StoreIntent(root, "tailnet", installation, true, nil, "0")
	require.NoError(t, err)
	require.NoError(t, RemoveIntent(root, "tailnet"))
	absent, err := LoadIntent(root, "tailnet")
	require.NoError(t, err)
	require.Equal(t, "0", absent.Generation)
	require.NoError(t, RemoveIntent(root, "tailnet"))
	require.Error(t, RemoveIntent(root, "../escape"))
}

func TestIntentPersistenceFailureHasNoEffect(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "tailnet")
	// An .intents path that cannot host files fails persistence outright.
	require.NoError(t, os.WriteFile(filepath.Join(root, ".intents"), []byte("blocked"), 0o600))
	_, err := StoreIntent(root, "tailnet", installation, true, nil, "0")
	require.ErrorContains(t, err, "unavailable")
	require.NoFileExists(t, IntentPath(root, "tailnet"))
	require.NoError(t, os.Remove(filepath.Join(root, ".intents")))

	// A validation failure leaves the recorded intent untouched.
	first, err := StoreIntent(root, "tailnet", installation, true, nil, "0")
	require.NoError(t, err)
	_, err = StoreIntent(root, "tailnet", installation, false, new(IntentActivate), "1")
	require.Error(t, err)
	loaded, err := LoadIntent(root, "tailnet")
	require.NoError(t, err)
	require.Equal(t, first, loaded)
}

func TestPackageStoreSharing(t *testing.T) {
	root := t.TempDir()
	first, err := OpenPackageStore(root)
	require.NoError(t, err)

	// A second handle is refused while the first owns the lock: offline
	// CLI mutations stay excluded while the manager runs.
	_, err = OpenPackageStore(root)
	require.ErrorContains(t, err, "stop Forgejo")

	require.NoError(t, first.Close())
	second, err := OpenPackageStore(root)
	require.NoError(t, err)
	require.NoError(t, second.Close())
}

func TestPackageStoreConcurrentSharing(t *testing.T) {
	root := t.TempDir()
	holder, err := OpenPackageStore(root)
	require.NoError(t, err)
	defer holder.Close()

	// While one holder owns the lock, every concurrent contender is
	// refused: CLI and manager veins cannot share a mutation.
	const racers = 8
	var refused atomic.Int32
	var group sync.WaitGroup
	for range racers {
		group.Go(func() {
			if _, err := OpenPackageStore(root); err != nil {
				refused.Add(1)
			}
		})
	}
	group.Wait()
	require.Equal(t, int32(racers), refused.Load(), "concurrent contenders must all be refused")
}

func TestStoppedHostCLIRefusedWhileManagerOwnsStore(t *testing.T) {
	root := t.TempDir()
	// The running manager's lifetime lock is the same mechanism CLI
	// commands contend with: holding it refuses OpenPackageStore.
	lock, err := AcquirePackageLock(root)
	require.NoError(t, err)
	defer lock.Close()
	_, err = OpenPackageStore(root)
	require.ErrorContains(t, err, "stop Forgejo")
}
