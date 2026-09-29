// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

// AcquirePackageLock excludes package changes while a manager is running.
// The lock is released by the OS on process exit; the lock file stays in place.
func AcquirePackageLock(root string) (*flock.Flock, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(root, ".packages.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	if !locked {
		_ = lock.Close()
		return nil, errors.New("extension packages are in use: stop Forgejo before changing packages")
	}
	return lock, nil
}

// Install copies a prebuilt package, never executing its contents. Existing
// packages require replace. Persistent extension data lives outside packages.
func Install(root, source string, replace bool) (Manifest, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Manifest{}, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return Manifest{}, err
	}
	if relative, err := filepath.Rel(source, root); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Manifest{}, errors.New("package source must not contain the installation directory")
	}
	lock, err := AcquirePackageLock(root)
	if err != nil {
		return Manifest{}, err
	}
	defer lock.Close()
	stage, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(stage)
	if err := copyPackage(source, stage); err != nil {
		return Manifest{}, err
	}
	manifest, err := LoadManifest(stage)
	if err != nil {
		return Manifest{}, err
	}
	if err := checkPackageFiles(stage, manifest); err != nil {
		return Manifest{}, err
	}
	target := filepath.Join(root, manifest.ID)
	if info, err := os.Lstat(target); err == nil {
		if !replace {
			return Manifest{}, fmt.Errorf("extension %q is already installed; use --replace to update it", manifest.ID)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Manifest{}, errors.New("installed package is not a regular directory")
		}
		if _, err := os.Lstat(filepath.Join(target, ".disabled")); err == nil {
			if err := os.WriteFile(filepath.Join(stage, ".disabled"), nil, 0o600); err != nil {
				return Manifest{}, err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Manifest{}, err
		}
		backup := stage + ".previous"
		if err := os.Rename(target, backup); err != nil {
			return Manifest{}, err
		}
		if err := os.Rename(stage, target); err != nil {
			return Manifest{}, errors.Join(err, os.Rename(backup, target))
		}
		if err := os.RemoveAll(backup); err != nil {
			return manifest, fmt.Errorf("package updated but old package cleanup failed: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, err
	} else if err := os.Rename(stage, target); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// SetEnabled changes next-start activation. It never deletes extension data.
func SetEnabled(root, id string, enabled bool) error {
	if !slug.MatchString(id) {
		return errors.New("invalid extension id")
	}
	lock, err := AcquirePackageLock(root)
	if err != nil {
		return err
	}
	defer lock.Close()
	directory := filepath.Join(root, id)
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("installed package is not a regular directory")
	}
	manifest, err := LoadManifest(directory)
	if err != nil {
		return err
	}
	if manifest.ID != id {
		return errors.New("installed directory does not match manifest id")
	}
	marker := filepath.Join(directory, ".disabled")
	if enabled {
		err = os.Remove(marker)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}

func copyPackage(source, target string) error {
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	var total int64
	files := 0
	return fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || name == "." {
			return walkErr
		}
		if name == ".disabled" {
			return errors.New("activation markers are managed by the installer")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package symlinks are not supported: %s", name)
		}
		if entry.IsDir() {
			return os.Mkdir(filepath.Join(target, name), 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("package contains a non-regular file: %s", name)
		}
		files++
		total += info.Size()
		if files > 10000 || total > 512<<20 {
			return errors.New("package exceeds 10000 files or 512 MiB")
		}
		in, err := root.Open(name)
		if err != nil {
			return err
		}
		defer in.Close()
		mode := fs.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		out, err := os.OpenFile(filepath.Join(target, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		copied, copyErr := io.Copy(out, io.LimitReader(in, info.Size()+1))
		closeErr := out.Close()
		if copied != info.Size() && copyErr == nil {
			copyErr = errors.New("package changed while copying")
		}
		return errors.Join(copyErr, closeErr)
	})
}

func checkPackageFiles(root string, manifest Manifest) error {
	names := []string{manifest.Executable}
	for _, page := range manifest.Pages {
		names = append(names, filepath.Join("assets", page.Entry))
	}
	for _, panel := range manifest.Panels {
		names = append(names, filepath.Join("assets", panel.Entry))
	}
	for i, name := range names {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || i == 0 && info.Mode()&0o111 == 0 {
			return fmt.Errorf("package file is missing or not executable: %s", name)
		}
	}
	return nil
}
