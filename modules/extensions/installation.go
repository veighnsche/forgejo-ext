// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	sdk "forgejo.org/extension-sdk"

	"github.com/google/uuid"
)

const installationProtocol = 1

type installationFile struct {
	Protocol       int    `json:"protocol"`
	InstallationID string `json:"installation_id"`
}

// InstallationPath returns the host-owned installation metadata for a package.
// Identity lives outside the package directory so replacement preserves it
// while removal deletes it.
func InstallationPath(root, id string) string {
	return filepath.Join(root, ".installations", id+".json")
}

// EnsureInstallation returns the stable installation UUID for a package,
// assigning a new random one when none is recorded. Replacement,
// enable/disable and process restart preserve it; removal deletes it so a
// later installation cannot adopt old operations.
func EnsureInstallation(root, id string) (string, error) {
	if !sdk.ValidID(id) {
		return "", errors.New("invalid extension id")
	}
	if existing, err := LoadInstallation(root, id); err == nil {
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	installation := uuid.NewString()
	document, err := json.Marshal(installationFile{Protocol: installationProtocol, InstallationID: installation})
	if err != nil {
		return "", err
	}
	directory := filepath.Join(root, ".installations")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", errors.New("extension installation directory unavailable")
	}
	path := InstallationPath(root, id)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return LoadInstallation(root, id)
	}
	if err != nil {
		return "", errors.New("extension installation identity unavailable")
	}
	_, writeErr := file.Write(document)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return "", errors.New("extension installation identity unavailable")
	}
	return installation, nil
}

// LoadInstallation returns the recorded installation UUID. A missing file is
// os.ErrNotExist; a malformed file is a hard error, never a silent new UUID.
func LoadInstallation(root, id string) (string, error) {
	if !sdk.ValidID(id) {
		return "", errors.New("invalid extension id")
	}
	file, err := os.Open(InstallationPath(root, id))
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4096))
	if err != nil {
		return "", errors.New("extension installation identity is invalid")
	}
	var document installationFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return "", errors.New("extension installation identity is invalid")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errors.New("extension installation identity is invalid")
	}
	if document.Protocol != installationProtocol {
		return "", errors.New("unsupported extension installation identity")
	}
	if _, err := uuid.Parse(document.InstallationID); err != nil {
		return "", errors.New("extension installation identity is invalid")
	}
	return document.InstallationID, nil
}

// RemoveInstallation deletes installation metadata. It is idempotent: a
// missing file is not an error.
func RemoveInstallation(root, id string) error {
	if !sdk.ValidID(id) {
		return errors.New("invalid extension id")
	}
	if err := os.Remove(InstallationPath(root, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
