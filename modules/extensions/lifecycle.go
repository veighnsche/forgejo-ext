// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"fmt"
	"strings"

	sdk "forgejo.org/extension-sdk"
)

// Draft manager-observation vocabulary (F01b scaffold, superseded by the
// RCX-2 frozen lifecycle wire in the SDK).
//
// This file preserves the pre-freeze manager request/result model with
// identical JSON and behavior so the current tree keeps compiling while
// its owners replace it: F02 replaces the persisted record with the frozen
// intent format, and F03 replaces the manager result model with controller
// observations (running/stopped/unknown plus admission grants). The frozen
// SDK wire (sdk.LifecycleCall, sdk.LifecycleObservation and their strict
// decoders) is the only lifecycle contract; nothing new may be built on
// the types below.
const (
	LifecycleDisabled     = "disabled"
	LifecycleActivating   = "activating"
	LifecycleEnabled      = "enabled"
	LifecycleDeactivating = "deactivating"
	LifecycleFailed       = "failed"
)

// Draft bounded error codes for the pre-freeze result model. The frozen
// wire uses a different code set (see sdk); these values are preserved for
// the current persisted records only.
const (
	LifecycleErrInvalidRequest  = "invalid_request"
	LifecycleErrNotInstalled    = "not_installed"
	LifecycleErrVersionMismatch = "version_mismatch"
	LifecycleErrUnavailable     = "unavailable"
	LifecycleErrInProgress      = "operation_in_progress"
	LifecycleErrRequiredPackage = "required_package"
	LifecycleErrAcquireFailed   = "acquire_failed"
	LifecycleErrActivateFailed  = "activate_failed"
	LifecycleErrTeardownFailed  = "teardown_failed"
	LifecycleErrUnconfirmed     = "unconfirmed"
	LifecycleErrStaleState      = "stale_state"
	LifecycleErrMaintenance     = "maintenance"
	LifecycleErrCatalogBinding  = "catalog_binding"
)

// LifecycleRequest identifies one draft manager operation on an installed
// package. PackageVersion pins the expected version when set; an empty
// version observes or acts on whatever is installed.
type LifecycleRequest struct {
	PackageID      string `json:"package_id"`
	PackageVersion string `json:"package_version,omitempty"`
	Operation      string `json:"operation"`
}

// LifecycleError is a bounded machine-readable failure with short human text.
type LifecycleError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// LifecycleResult reports one draft manager operation outcome. State is one
// of the Lifecycle* UI states above; Unconfirmed marks an unknown effect
// that must be re-observed before retry. Progress carries short
// human-readable detail.
type LifecycleResult struct {
	Operation       string          `json:"operation"`
	PackageID       string          `json:"package_id"`
	ObservedVersion string          `json:"observed_version,omitempty"`
	State           string          `json:"state"`
	Unconfirmed     bool            `json:"unconfirmed,omitempty"`
	Progress        string          `json:"progress,omitempty"`
	Error           *LifecycleError `json:"error,omitempty"`
}

// FailedResult builds a definite failed result for one operation.
func FailedResult(operation, id, code, message string) LifecycleResult {
	return LifecycleResult{
		Operation: operation,
		PackageID: id,
		State:     LifecycleFailed,
		Error:     &LifecycleError{Code: code, Message: message},
	}
}

// UnconfirmedResult builds a failed result whose effect is unknown: the
// caller must re-observe before retrying.
func UnconfirmedResult(operation, id, code, message string) LifecycleResult {
	result := FailedResult(operation, id, code, message)
	result.Unconfirmed = true
	return result
}

func validDraftLifecycleOperation(operation string) bool {
	return operation == sdk.LifecycleObserve || operation == sdk.LifecycleActivate || operation == sdk.LifecycleDeactivate
}

func validDraftLifecycleState(state string) bool {
	switch state {
	case LifecycleDisabled, LifecycleActivating, LifecycleEnabled, LifecycleDeactivating, LifecycleFailed:
		return true
	}
	return false
}

func validDraftLifecycleCode(code string) bool {
	switch code {
	case LifecycleErrInvalidRequest, LifecycleErrNotInstalled, LifecycleErrVersionMismatch,
		LifecycleErrUnavailable, LifecycleErrInProgress, LifecycleErrRequiredPackage,
		LifecycleErrAcquireFailed, LifecycleErrActivateFailed, LifecycleErrTeardownFailed,
		LifecycleErrUnconfirmed, LifecycleErrStaleState, LifecycleErrMaintenance,
		LifecycleErrCatalogBinding:
		return true
	}
	return false
}

// ValidateLifecycleRequest checks the structural draft request rules.
func ValidateLifecycleRequest(request LifecycleRequest) error {
	if !sdk.ValidID(request.PackageID) {
		return errors.New("invalid lifecycle package id")
	}
	if !validDraftLifecycleOperation(request.Operation) {
		return errors.New("invalid lifecycle operation")
	}
	if len(request.PackageVersion) > 64 {
		return errors.New("invalid lifecycle package version")
	}
	return nil
}

// ValidateLifecycleResult checks the structural draft result rules,
// including bounded error codes and message sizes.
func ValidateLifecycleResult(result LifecycleResult) error {
	if !sdk.ValidID(result.PackageID) {
		return errors.New("invalid lifecycle result package id")
	}
	if !validDraftLifecycleOperation(result.Operation) {
		return errors.New("invalid lifecycle result operation")
	}
	if !validDraftLifecycleState(result.State) {
		return errors.New("invalid lifecycle result state")
	}
	if len(result.ObservedVersion) > 64 || len(result.Progress) > 512 {
		return errors.New("invalid lifecycle result detail")
	}
	if result.State == LifecycleFailed && result.Error == nil {
		return errors.New("failed lifecycle result requires an error")
	}
	if result.Error != nil {
		if !validDraftLifecycleCode(result.Error.Code) || strings.TrimSpace(result.Error.Message) == "" || len(result.Error.Message) > 512 {
			return fmt.Errorf("invalid lifecycle error %q", result.Error.Code)
		}
	}
	return nil
}
