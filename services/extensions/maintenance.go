// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// maintenanceMarker is the updater's persistent record. Its presence
// closes optional admission after reboot. Fountain never removes the
// marker and never parses its content.
const maintenanceMarker = "operation.json"

// acquireMaintenanceGuard takes the shared directory-inode guard for one
// intent, acquisition, and lifecycle operation until its durable terminal
// or pending outcome. It opens the configured directory with
// O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC, verifies exact root ownership and
// mode 0755, takes LOCK_SH|LOCK_NB, then requires operation.json absent.
// An empty directory means standalone Fountain with no guard. Unreadable
// metadata is unavailable, never absence. No .lock file is created: root
// maintenance takes LOCK_EX on the identical directory inode for its
// entire operation.
func acquireMaintenanceGuard(dir string) (release func(), err error) {
	nop := func() {}
	if dir == "" {
		return nop, nil
	}
	file, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("maintenance directory is unavailable")
	}
	release = func() { _ = file.Close() }
	if !maintenanceMetadataValid(file) {
		release()
		return nil, errors.New("maintenance directory is unsafe")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		release()
		return nil, errors.New("maintenance holds exclusive exclusion")
	}
	if _, err := os.Lstat(filepath.Join(dir, maintenanceMarker)); err == nil {
		release()
		return nil, errors.New("maintenance marker is present")
	} else if !errors.Is(err, os.ErrNotExist) {
		release()
		return nil, errors.New("maintenance state is unreadable")
	}
	return release, nil
}

// maintenanceHeld probes whether optional effects are fenced without
// acquiring the shared lock: invalid directory metadata, a present
// marker, an updater-held exclusive lock, or an unreadable directory
// (all fail-closed). The non-blocking probe never deadlocks against the
// updater's exclusive exclusion, so state and snapshot reads use it
// while the updater holds that lock.
func maintenanceHeld(dir string) bool {
	if dir == "" {
		return false
	}
	file, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return true
	}
	defer file.Close()
	// The fence is only meaningful on the exact production directory
	// contract: root-owned mode 0755. Anything else cannot prove the
	// updater's exclusive inode exclusion, so it stays fenced.
	if !maintenanceMetadataValid(file) {
		return true
	}
	if _, err := os.Lstat(filepath.Join(dir, maintenanceMarker)); err == nil {
		return true
	} else if !errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}

// maintenanceMetadataValid reports the exact directory-inode contract:
// a directory, root-owned, mode 0755. It validates the already-open
// descriptor so the identity cannot change between open and check.
func maintenanceMetadataValid(file *os.File) bool {
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		return false
	}
	if info.Mode().Perm() != 0o755 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return false
	}
	return true
}
