// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCrashBarrierInactiveWithoutEnv(t *testing.T) {
	t.Setenv("NATIVEOP_TEST_CRASH_POINT", "")
	t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", "")
	require.NoError(t, TestCrashBarrier(CrashPointBranchDeleteAfterEffects))
}

func TestCrashBarrierIgnoresOtherPoints(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NATIVEOP_TEST_CRASH_POINT", CrashPointMergeAfterNative)
	t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", dir)
	require.NoError(t, TestCrashBarrier(CrashPointBranchDeleteAfterEffects))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestCrashBarrierNeedsDirectory(t *testing.T) {
	t.Setenv("NATIVEOP_TEST_CRASH_POINT", CrashPointBranchDeleteAfterEffects)
	t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", "")
	require.NoError(t, TestCrashBarrier(CrashPointBranchDeleteAfterEffects))
}

func TestCrashBarrierPassesOnRelease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NATIVEOP_TEST_CRASH_POINT", CrashPointBranchDeleteAfterEffects)
	t.Setenv("NATIVEOP_TEST_CRASH_BARRIER_DIR", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, CrashPointBranchDeleteAfterEffects+".release"), []byte("go\n"), 0o600))
	require.NoError(t, TestCrashBarrier(CrashPointBranchDeleteAfterEffects))
	entered, err := os.ReadFile(filepath.Join(dir, CrashPointBranchDeleteAfterEffects+".entered"))
	require.NoError(t, err)
	require.NotEmpty(t, entered)
}
