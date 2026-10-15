// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	sdk "forgejo.org/extension-sdk"

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

// receiptFileName is the reserved generated receipt. Neither archives nor
// prebuilt source directories may supply it; the store generates it in
// the private staged directory after byte verification.
const receiptFileName = ".fountain-package.json"

// Receipt identifies one installed artifact: schema, package identity
// and the verified archive digest. It travels atomically with the
// package directory. It does not authorize the package independently of
// the trusted catalog. Installation UUID stays outside package bytes in
// .installations, and the receipt stays outside assets/ where the asset
// handler never serves it.
type Receipt struct {
	Schema         int    `json:"schema"`
	PackageID      string `json:"package_id"`
	PackageVersion string `json:"package_version"`
	ArchiveSHA256  string `json:"archive_sha256"`
}

// WriteReceipt generates the receipt in a private staged directory after
// the archive bytes are verified and the manifest is matched.
func WriteReceipt(stage string, entry sdk.CatalogPackage) error {
	data, err := json.Marshal(Receipt{Schema: 1, PackageID: entry.ID, PackageVersion: entry.Version, ArchiveSHA256: entry.SHA256})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stage, receiptFileName), data, 0o600)
}

// LoadReceipt reads and strictly validates a generated receipt.
func LoadReceipt(packageDir string) (Receipt, error) {
	file, err := os.Open(filepath.Join(packageDir, receiptFileName))
	if err != nil {
		return Receipt{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximumIntentBytes+1))
	if err != nil {
		return Receipt{}, errors.New("extension package receipt is invalid")
	}
	var receipt Receipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return Receipt{}, errors.New("extension package receipt is invalid")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Receipt{}, errors.New("extension package receipt is invalid")
	}
	if receipt.Schema != 1 || !sdk.ValidID(receipt.PackageID) {
		return Receipt{}, errors.New("extension package receipt is invalid")
	}
	return receipt, nil
}

// ValidateReceipt checks an installed receipt against its catalog pin.
func ValidateReceipt(packageDir string, entry sdk.CatalogPackage) error {
	receipt, err := LoadReceipt(packageDir)
	if err != nil {
		return err
	}
	if receipt.PackageID != entry.ID || receipt.PackageVersion != entry.Version || receipt.ArchiveSHA256 != entry.SHA256 {
		return errors.New("installed package receipt does not match catalog")
	}
	return nil
}

// ValidateInstalled checks that an installed directory exactly satisfies
// one catalog entry: generated receipt, manifest identity, capability
// and controller match, and package files. It is the reuse gate for
// required bootstrap, offline reboot and maintenance preparation.
func ValidateInstalled(root string, entry sdk.CatalogPackage) error {
	directory := filepath.Join(root, entry.ID)
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("installed package is not a regular directory")
	}
	if err := ValidateReceipt(directory, entry); err != nil {
		return err
	}
	manifest, err := sdk.LoadManifest(directory)
	if err != nil {
		return err
	}
	if err := MatchEntryManifest(entry, manifest); err != nil {
		return err
	}
	return checkPackageFiles(directory, manifest)
}

// reservedStagedNames are installer-managed files no source may supply.
var reservedStagedNames = map[string]bool{receiptFileName: true, ".disabled": true}

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
		if reservedStagedNames[name] {
			return errors.New("reserved package state is managed by the installer")
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

func checkPackageFiles(root string, manifest sdk.Manifest) error {
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

// EvictCache removes one exact corrupt cache entry without touching other
// entries, so it can be reacquired. It is idempotent.
func EvictCache(root, sha string) error {
	path := cachePath(root, sha)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
