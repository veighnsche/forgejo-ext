// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues

import (
	"sort"
)

// Snapshot conversion for native assignee evidence (FT14, early start).
//
// AssigneePage converts loader-read assignee IDs for one issue into
// permission-checked evidence with completeness. IDs sort ascending so the
// page is deterministic regardless of loader row order. Like the other
// snapshots this performs no database reads: the caller supplies current IDs
// with the actor's visibility decision. A hidden page preserves only the
// issue locator. Authoritative guarantees wait for FT09 plus the
// revision-bracket proof; until then this is conversion/DTO work only.

// AssigneePage carries one bounded assignee list with completeness evidence.
type AssigneePage struct {
	IssueID      int64
	AssigneeIDs  []int64
	Total        int
	Visible      bool
	HiddenReason string
	Complete     bool
}

// NewAssigneePage validates one loader-read assignee list. Complete requires
// every Total ID Returned. A hidden page keeps only the issue locator.
func NewAssigneePage(issueID int64, assigneeIDs []int64, total int, visible bool, hiddenReason string, complete bool) (AssigneePage, error) {
	if issueID <= 0 || total < 0 || len(assigneeIDs) > total {
		return AssigneePage{}, ErrSnapshotInvalid
	}
	if !visible {
		if !validHiddenReason(hiddenReason) {
			return AssigneePage{}, ErrSnapshotInvalid
		}
		return AssigneePage{
			IssueID:      issueID,
			Total:        total,
			Visible:      false,
			HiddenReason: hiddenReason,
			Complete:     complete,
		}, nil
	}
	if hiddenReason != "" {
		return AssigneePage{}, ErrSnapshotInvalid
	}
	for _, id := range assigneeIDs {
		if id <= 0 {
			return AssigneePage{}, ErrSnapshotInvalid
		}
	}
	if complete && len(assigneeIDs) != total {
		return AssigneePage{}, ErrSnapshotInvalid
	}
	sorted := append([]int64(nil), assigneeIDs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return AssigneePage{
		IssueID:     issueID,
		AssigneeIDs: sorted,
		Total:       total,
		Visible:     true,
		Complete:    complete,
	}, nil
}
