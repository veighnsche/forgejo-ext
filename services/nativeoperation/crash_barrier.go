// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Crash barrier points for offline-recovery proof. Each point sits after a
// writer's effects are committed and before its owner releases.
const (
	// CrashPointBranchDeleteAfterEffects pauses DeleteBranch after its
	// database transaction and direct Git delete commit.
	CrashPointBranchDeleteAfterEffects = "branch-delete-after-effects"
	// CrashPointActionsTaskAfterEffects pauses one Actions task/job update
	// after its task, job and resulting status effects commit.
	CrashPointActionsTaskAfterEffects = "actions-task-after-effects"
	// CrashPointMergeAfterNative pauses a conditional merge after the
	// native merge child returns and before reconciliation.
	CrashPointMergeAfterNative = "merge-after-native"
	// CrashPointMergeBeforeNative pauses a conditional merge after the
	// claim and before the native merge starts, so a cancellation race
	// can be staged deterministically against prepared admission and a
	// crash before any effect can be recovered offline.
	CrashPointMergeBeforeNative = "merge-before-native"
	// CrashPointPushCompletionAfterEffects pauses one deferred
	// push/completion batch after its effects commit.
	CrashPointPushCompletionAfterEffects = "push-completion-after-effects"
	// CrashPointPRCreateBeforePrimary pauses a conditional PR creation
	// after the claim and before the atomic primary commit, so a
	// cancellation race can be staged deterministically against it.
	CrashPointPRCreateBeforePrimary = "prcreate-before-primary"
	// CrashPointPRCreateAfterPrimary pauses a conditional PR creation
	// after its primary issue/PR commit and before bounded completion,
	// with the owner still held for offline-recovery proof.
	CrashPointPRCreateAfterPrimary = "prcreate-after-primary"
	// CrashPointReviewSubmitBeforePrimary pauses a conditional review
	// submission after the claim and before the atomic primary commit,
	// so a cancellation race can be staged deterministically against it.
	CrashPointReviewSubmitBeforePrimary = "review-submit-before-primary"
	// CrashPointReviewSubmitAfterPrimary pauses a conditional review
	// submission after its primary review commit and before bounded
	// completion, with the owner still held for offline-recovery proof.
	CrashPointReviewSubmitAfterPrimary = "review-submit-after-primary"
)

// TestCrashBarrier is a disclosed test instrument for offline-recovery
// proof, mirroring the M1b admission barrier. When
// NATIVEOP_TEST_CRASH_POINT names point and
// NATIVEOP_TEST_CRASH_BARRIER_DIR names a directory, it writes
// <point>.entered and waits for <point>.release (bounded). Production
// never sets these variables. The proof driver SIGKILLs the server once
// .entered appears, simulating a crash with effects committed and the
// owner still held; recovery then runs offline in a stopped domain.
func TestCrashBarrier(point string) error {
	if os.Getenv("NATIVEOP_TEST_CRASH_POINT") != point {
		return nil
	}
	dir := os.Getenv("NATIVEOP_TEST_CRASH_BARRIER_DIR")
	if dir == "" {
		return nil
	}
	if err := os.WriteFile(filepath.Join(dir, point+".entered"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return err
	}
	deadline := time.Now().Add(crashBarrierTimeout())
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, point+".release")); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("crash barrier timeout for " + point)
}

// crashBarrierTimeout bounds one barrier wait. Tests shorten it with
// NATIVEOP_TEST_CRASH_TIMEOUT_SECONDS to fail a held owner closed without
// waiting out the production-absent default; production never sets these
// variables.
func crashBarrierTimeout() time.Duration {
	if raw := os.Getenv("NATIVEOP_TEST_CRASH_TIMEOUT_SECONDS"); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 120 * time.Second
}
