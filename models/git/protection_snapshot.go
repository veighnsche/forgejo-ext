// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"strings"
)

// Snapshot conversion for native branch-protection evidence (FT14, early start).
//
// ProtectionSnapshot converts one already-loaded protected-branch rule into
// permission-checked evidence carrying the exact policy inputs the merge path
// consumes: merge whitelist, required status checks, approval rules, review
// blocks, signed-commit and protected-file requirements and admin scope. Push
// whitelist fields are included so the snapshot is the complete rule, not a
// merge-only projection with its own semantics. Like the other snapshots this
// performs no database reads: the caller resolves the current matching rule
// for the live base branch with the actor's visibility decision, and the
// conversion enforces redaction for hidden rules. Authoritative guarantees
// wait for FT09 plus the revision-bracket proof; until then this is
// conversion/DTO work only, not F-read.

// ProtectionSnapshot is the permission-checked view of one native
// protected-branch rule. MatchedBranch is the short branch name the caller
// matched this rule against.
type ProtectionSnapshot struct {
	RuleID                        int64
	RepositoryID                  int64
	RuleName                      string
	MatchedBranch                 string
	CanPush                       bool
	EnableWhitelist               bool
	WhitelistUserIDs              []int64
	WhitelistTeamIDs              []int64
	EnableMergeWhitelist          bool
	WhitelistDeployKeys           bool
	MergeWhitelistUserIDs         []int64
	MergeWhitelistTeamIDs         []int64
	EnableStatusCheck             bool
	StatusCheckContexts           []string
	EnableApprovalsWhitelist      bool
	ApprovalsWhitelistUserIDs     []int64
	ApprovalsWhitelistTeamIDs     []int64
	RequiredApprovals             int64
	BlockOnRejectedReviews        bool
	BlockOnOfficialReviewRequests bool
	BlockOnOutdatedBranch         bool
	DismissStaleApprovals         bool
	IgnoreStaleApprovals          bool
	RequireSignedCommits          bool
	ProtectedFilePatterns         string
	UnprotectedFilePatterns       string
	ApplyToAdmins                 bool
	UpdatedUnix                   int64
	Visible                       bool
	HiddenReason                  string
	Complete                      bool
}

// NewProtectionSnapshot converts one current protected-branch rule. A hidden
// rule preserves only the repository/rule locators the caller supplied.
func NewProtectionSnapshot(rule *ProtectedBranch, baseBranch string, visible bool, hiddenReason string, complete bool) (ProtectionSnapshot, error) {
	if rule == nil || rule.ID <= 0 || rule.RepoID <= 0 || strings.TrimSpace(rule.RuleName) == "" || len(rule.RuleName) > 255 {
		return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
	}
	if strings.TrimSpace(baseBranch) == "" || len(baseBranch) > 512 || strings.ContainsAny(baseBranch, " ~^:?*\\") || strings.Contains(baseBranch, "..") {
		return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
	}
	if !visible {
		if !validCheckHiddenReason(hiddenReason) {
			return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
		}
		return ProtectionSnapshot{
			RuleID:       rule.ID,
			RepositoryID: rule.RepoID,
			RuleName:     rule.RuleName,
			Visible:      false,
			HiddenReason: hiddenReason,
			Complete:     complete,
		}, nil
	}
	if hiddenReason != "" {
		return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
	}
	if rule.RequiredApprovals < 0 {
		return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
	}
	for _, id := range rule.WhitelistUserIDs {
		if id <= 0 {
			return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
		}
	}
	for _, id := range rule.WhitelistTeamIDs {
		if id <= 0 {
			return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
		}
	}
	for _, id := range rule.MergeWhitelistUserIDs {
		if id <= 0 {
			return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
		}
	}
	for _, id := range rule.MergeWhitelistTeamIDs {
		if id <= 0 {
			return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
		}
	}
	for _, id := range rule.ApprovalsWhitelistUserIDs {
		if id <= 0 {
			return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
		}
	}
	for _, id := range rule.ApprovalsWhitelistTeamIDs {
		if id <= 0 {
			return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
		}
	}
	for _, context := range rule.StatusCheckContexts {
		if strings.TrimSpace(context) == "" {
			return ProtectionSnapshot{}, ErrCheckSnapshotInvalid
		}
	}
	return ProtectionSnapshot{
		RuleID:                        rule.ID,
		RepositoryID:                  rule.RepoID,
		RuleName:                      rule.RuleName,
		MatchedBranch:                 baseBranch,
		CanPush:                       rule.CanPush,
		EnableWhitelist:               rule.EnableWhitelist,
		WhitelistUserIDs:              append([]int64(nil), rule.WhitelistUserIDs...),
		WhitelistTeamIDs:              append([]int64(nil), rule.WhitelistTeamIDs...),
		EnableMergeWhitelist:          rule.EnableMergeWhitelist,
		WhitelistDeployKeys:           rule.WhitelistDeployKeys,
		MergeWhitelistUserIDs:         append([]int64(nil), rule.MergeWhitelistUserIDs...),
		MergeWhitelistTeamIDs:         append([]int64(nil), rule.MergeWhitelistTeamIDs...),
		EnableStatusCheck:             rule.EnableStatusCheck,
		StatusCheckContexts:           append([]string(nil), rule.StatusCheckContexts...),
		EnableApprovalsWhitelist:      rule.EnableApprovalsWhitelist,
		ApprovalsWhitelistUserIDs:     append([]int64(nil), rule.ApprovalsWhitelistUserIDs...),
		ApprovalsWhitelistTeamIDs:     append([]int64(nil), rule.ApprovalsWhitelistTeamIDs...),
		RequiredApprovals:             rule.RequiredApprovals,
		BlockOnRejectedReviews:        rule.BlockOnRejectedReviews,
		BlockOnOfficialReviewRequests: rule.BlockOnOfficialReviewRequests,
		BlockOnOutdatedBranch:         rule.BlockOnOutdatedBranch,
		DismissStaleApprovals:         rule.DismissStaleApprovals,
		IgnoreStaleApprovals:          rule.IgnoreStaleApprovals,
		RequireSignedCommits:          rule.RequireSignedCommits,
		ProtectedFilePatterns:         rule.ProtectedFilePatterns,
		UnprotectedFilePatterns:       rule.UnprotectedFilePatterns,
		ApplyToAdmins:                 rule.ApplyToAdmins,
		UpdatedUnix:                   int64(rule.UpdatedUnix),
		Visible:                       true,
		Complete:                      complete,
	}, nil
}
