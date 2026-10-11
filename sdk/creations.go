// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"strconv"
)

const CreationRecoveryPageLimit = 100

// CreationRecoveryRequest selects retained native issue-event tasks for one
// credential-bound repository. AfterID is the last scanned native task ID.
type CreationRecoveryRequest struct {
	RepositoryID string `json:"repository_id"`
	ActorID      string `json:"actor_id"`
	AfterID      string `json:"after_id,omitempty"`
	Limit        int    `json:"limit"`
}

func ValidateCreationRecoveryRequest(req CreationRecoveryRequest) error {
	if !creationNativeID(req.RepositoryID) || !creationNativeID(req.ActorID) ||
		(req.AfterID != "" && !creationNativeID(req.AfterID)) || req.Limit < 1 || req.Limit > CreationRecoveryPageLimit {
		return errors.New("invalid native creation recovery request")
	}
	return nil
}

func creationNativeID(value string) bool {
	if !snapshotDecimalID(value) {
		return false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0
}

// NativeCreationObservation carries original native creation facts, never raw
// hook payloads or secrets. Verified requires current native identity and
// retained creation provenance; it does not assert current input acceptance.
type NativeCreationObservation struct {
	TaskID        string `json:"task_id"`
	DeliveryID    string `json:"delivery_id"`
	RepositoryID  string `json:"repository_id"`
	IssueID       string `json:"issue_id"`
	IssueIndex    string `json:"issue_index"`
	CreatorID     string `json:"creator_id"`
	CreatorWrite  bool   `json:"creator_write"`
	CreatedUnix   int64  `json:"created_unix"`
	TitleDigest   string `json:"title_digest"`
	ContentDigest string `json:"content_digest"`
	Verified      bool   `json:"verified"`
}

// CreationRecoveryPage reports a bounded scan of retained tasks. Complete
// means the scan reached the retained end, not that all creation history exists.
// ProvenanceComplete distinguishes unsupported/redacted records from absence.
// RetentionLimited and OldestRetainedTaskID expose the native retention boundary.
type CreationRecoveryPage struct {
	RepositoryID         string                      `json:"repository_id"`
	Creations            []NativeCreationObservation `json:"creations"`
	NextCursor           string                      `json:"next_cursor,omitempty"`
	OldestRetainedTaskID string                      `json:"oldest_retained_task_id,omitempty"`
	Complete             bool                        `json:"complete"`
	ProvenanceComplete   bool                        `json:"provenance_complete"`
	RetentionLimited     bool                        `json:"retention_limited"`
}
