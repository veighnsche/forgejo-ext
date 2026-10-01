// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"strconv"
	"strings"
)

// Permission-checked native snapshot reads (FT10, F-read).
//
// A SnapshotRequest selects bounded native families; the host answers with a
// NativeSnapshot carrying exact versions, creation/title/lifecycle evidence,
// edge occurrences, visibility and completeness. Hidden records are redacted
// to their locator keys with Visible=false so the consumer blocks work
// without leaking inaccessible content.
//
// The snapshot carries no revision: the caller brackets the read between two
// equal idle ReadNativeRevision observations and binds that revision itself.
// A changed/busy observation, a missing page or an incomplete section cannot
// authorize work. No factory policy is interpreted here: no acceptance,
// readiness, budget or approval fields.

// Snapshot families select bounded native evidence. Unknown families refuse.
const (
	SnapshotFamilyIssue        = "issue"
	SnapshotFamilyComments     = "comments"
	SnapshotFamilyDependencies = "dependencies"
	SnapshotFamilyPull         = "pull"
	SnapshotFamilyReviews      = "reviews"
	SnapshotFamilyChecks       = "checks"
	SnapshotFamilyRefs         = "refs"
)

// Snapshot bounds keep reads bounded and attributable.
const (
	SnapshotPageLimit   = 50
	SnapshotIDListLimit = 50
	SnapshotRefLimit    = 8
	SnapshotCursorLimit = 4096
)

// ErrInvalidSnapshotRequest rejects malformed requests rather than silently
// broadening or defaulting them.
var ErrInvalidSnapshotRequest = errors.New("invalid native snapshot request")

// SnapshotRequest selects bounded native evidence. Native integer IDs use
// decimal strings; refs are full branch refs; SHAs are full object IDs.
// ActorID names the bound native actor whose credential is presented
// privately alongside the request; the host verifies the credential belongs
// to that actor. Cursor is an opaque decimal after-ID for list families:
// items carry IDs strictly greater than the cursor, ordered ascending.
type SnapshotRequest struct {
	RepositoryID string   `json:"repository_id"`
	ActorID      string   `json:"actor_id"`
	Families     []string `json:"families"`
	IssueIndex   string   `json:"issue_index,omitempty"`
	PullNumber   string   `json:"pull_number,omitempty"`
	CommentIDs   []string `json:"comment_ids,omitempty"`
	SHA          string   `json:"sha,omitempty"`
	Refs         []string `json:"refs,omitempty"`
	Limit        int      `json:"limit,omitempty"`
	Cursor       string   `json:"cursor,omitempty"`
}

func validSnapshotFamily(family string) bool {
	switch family {
	case SnapshotFamilyIssue, SnapshotFamilyComments, SnapshotFamilyDependencies,
		SnapshotFamilyPull, SnapshotFamilyReviews, SnapshotFamilyChecks, SnapshotFamilyRefs:
		return true
	default:
		return false
	}
}

func snapshotDecimalID(id string) bool {
	if id == "" || id[0] == '0' || len(id) > 20 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	_, err := strconv.ParseInt(id, 10, 64)
	return err == nil
}

func snapshotFullOID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	for _, c := range oid {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
			continue
		}
		return false
	}
	return true
}

func snapshotFullBranchRef(ref string) bool {
	if !strings.HasPrefix(ref, "refs/heads/") || len(ref) > 512 {
		return false
	}
	name := strings.TrimSpace(ref)
	if name != ref || strings.ContainsAny(ref, " ~^:?*\\") || strings.Contains(ref, "..") {
		return false
	}
	return len(strings.TrimPrefix(ref, "refs/heads/")) > 0
}

// ValidateSnapshotRequest bounds the requested families and locators. Each
// family requires its selector so the host never guesses which records the
// caller means: issue/comments/dependencies need the issue (or explicit
// comment IDs), pull/reviews need the pull or issue, checks need the commit,
// refs need the ref list. Comment selection is either an issue list or an
// explicit ID list, never both; review selection is either an issue or a
// pull, never both.
func ValidateSnapshotRequest(req SnapshotRequest) error {
	if !snapshotDecimalID(req.RepositoryID) || !snapshotDecimalID(req.ActorID) {
		return ErrInvalidSnapshotRequest
	}
	if len(req.Families) == 0 || len(req.Families) > 7 {
		return ErrInvalidSnapshotRequest
	}
	seen := make(map[string]bool, len(req.Families))
	for _, family := range req.Families {
		if !validSnapshotFamily(family) || seen[family] {
			return ErrInvalidSnapshotRequest
		}
		seen[family] = true
	}
	if req.IssueIndex != "" && !snapshotDecimalID(req.IssueIndex) {
		return ErrInvalidSnapshotRequest
	}
	if req.PullNumber != "" && !snapshotDecimalID(req.PullNumber) {
		return ErrInvalidSnapshotRequest
	}
	if len(req.CommentIDs) > SnapshotIDListLimit {
		return ErrInvalidSnapshotRequest
	}
	for _, id := range req.CommentIDs {
		if !snapshotDecimalID(id) {
			return ErrInvalidSnapshotRequest
		}
	}
	if req.SHA != "" && !snapshotFullOID(req.SHA) {
		return ErrInvalidSnapshotRequest
	}
	if len(req.Refs) > SnapshotRefLimit {
		return ErrInvalidSnapshotRequest
	}
	for _, ref := range req.Refs {
		if !snapshotFullBranchRef(ref) {
			return ErrInvalidSnapshotRequest
		}
	}
	if req.Limit < 0 || req.Limit > SnapshotPageLimit || len(req.Cursor) > SnapshotCursorLimit {
		return ErrInvalidSnapshotRequest
	}
	if req.Cursor != "" && !snapshotDecimalID(req.Cursor) {
		return ErrInvalidSnapshotRequest
	}
	if seen[SnapshotFamilyRefs] && len(req.Refs) == 0 {
		return ErrInvalidSnapshotRequest
	}
	if seen[SnapshotFamilyChecks] && req.SHA == "" {
		return ErrInvalidSnapshotRequest
	}
	if seen[SnapshotFamilyIssue] && req.IssueIndex == "" {
		return ErrInvalidSnapshotRequest
	}
	if seen[SnapshotFamilyDependencies] && req.IssueIndex == "" {
		return ErrInvalidSnapshotRequest
	}
	if seen[SnapshotFamilyPull] && req.PullNumber == "" {
		return ErrInvalidSnapshotRequest
	}
	if seen[SnapshotFamilyComments] {
		byIssue := req.IssueIndex != ""
		byIDs := len(req.CommentIDs) > 0
		if byIssue == byIDs {
			return ErrInvalidSnapshotRequest
		}
	}
	if seen[SnapshotFamilyReviews] {
		byIssue := req.IssueIndex != ""
		byPull := req.PullNumber != ""
		if byIssue == byPull {
			return ErrInvalidSnapshotRequest
		}
	}
	return nil
}

// SnapshotLifecycleEvent is one title/lifecycle transition.
type SnapshotLifecycleEvent struct {
	Kind     string `json:"kind"`
	AtUnix   int64  `json:"at_unix"`
	ActorID  string `json:"actor_id"`
	OldTitle string `json:"old_title,omitempty"`
	NewTitle string `json:"new_title,omitempty"`
}

// SnapshotCreationProvenance binds verified creation evidence. Verified
// requires visibility plus the native first-created flag; cached text alone
// never verifies provenance.
type SnapshotCreationProvenance struct {
	PosterID     string `json:"poster_id,omitempty"`
	CreatedUnix  int64  `json:"created_unix,omitempty"`
	FirstCreated bool   `json:"first_created,omitempty"`
	Verified     bool   `json:"verified,omitempty"`
}

// SnapshotIssue is the permission-checked view of one native issue. Hidden
// issues preserve only their locator IDs with Visible=false.
type SnapshotIssue struct {
	ID             string                     `json:"id"`
	Index          string                     `json:"index"`
	Title          string                     `json:"title,omitempty"`
	Content        string                     `json:"content,omitempty"`
	TitleDigest    string                     `json:"title_digest,omitempty"`
	ContentDigest  string                     `json:"content_digest,omitempty"`
	ContentVersion int                        `json:"content_version,omitempty"`
	NumComments    int                        `json:"num_comments,omitempty"`
	IsClosed       bool                       `json:"is_closed,omitempty"`
	IsLocked       bool                       `json:"is_locked,omitempty"`
	IsPull         bool                       `json:"is_pull,omitempty"`
	Provenance     SnapshotCreationProvenance `json:"provenance"`
	Lifecycle      []SnapshotLifecycleEvent   `json:"lifecycle,omitempty"`
	CreatedUnix    int64                      `json:"created_unix,omitempty"`
	UpdatedUnix    int64                      `json:"updated_unix,omitempty"`
	ClosedUnix     int64                      `json:"closed_unix,omitempty"`
	Visible        bool                       `json:"visible"`
	HiddenReason   string                     `json:"hidden_reason,omitempty"`
	Complete       bool                       `json:"complete"`
}

// SnapshotComment is the permission-checked view of one native comment.
type SnapshotComment struct {
	ID             string `json:"id"`
	IssueID        string `json:"issue_id"`
	Type           string `json:"type,omitempty"`
	PosterID       string `json:"poster_id,omitempty"`
	Content        string `json:"content,omitempty"`
	ContentDigest  string `json:"content_digest,omitempty"`
	ContentVersion int    `json:"content_version,omitempty"`
	ReviewID       string `json:"review_id,omitempty"`
	Invalidated    bool   `json:"invalidated,omitempty"`
	CreatedUnix    int64  `json:"created_unix,omitempty"`
	UpdatedUnix    int64  `json:"updated_unix,omitempty"`
	Visible        bool   `json:"visible"`
	HiddenReason   string `json:"hidden_reason,omitempty"`
	Complete       bool   `json:"complete"`
}

// SnapshotCommentPage carries one bounded comment list with completeness
// evidence.
type SnapshotCommentPage struct {
	IssueID  string            `json:"issue_id"`
	Items    []SnapshotComment `json:"items"`
	Total    int               `json:"total"`
	Complete bool              `json:"complete"`
}

// SnapshotDependency is one edge occurrence. OccurrenceID distinguishes
// removal plus readdition of the same edge; hidden occurrences redact the
// target so an inaccessible prerequisite blocks without leaking it.
type SnapshotDependency struct {
	OccurrenceID string `json:"occurrence_id"`
	IssueID      string `json:"issue_id"`
	DependencyID string `json:"dependency_id,omitempty"`
	CreatedUnix  int64  `json:"created_unix,omitempty"`
	UpdatedUnix  int64  `json:"updated_unix,omitempty"`
	Visible      bool   `json:"visible"`
	HiddenReason string `json:"hidden_reason,omitempty"`
	Complete     bool   `json:"complete"`
}

// SnapshotDependencyPage carries one bounded edge list with completeness
// evidence.
type SnapshotDependencyPage struct {
	IssueID  string               `json:"issue_id"`
	Items    []SnapshotDependency `json:"items"`
	Total    int                  `json:"total"`
	Complete bool                 `json:"complete"`
}

// SnapshotPull is the permission-checked view of one native pull request.
type SnapshotPull struct {
	ID           string `json:"id"`
	IssueID      string `json:"issue_id"`
	Number       string `json:"number"`
	HeadRepoID   string `json:"head_repo_id,omitempty"`
	HeadBranch   string `json:"head_branch,omitempty"`
	HeadTip      string `json:"head_tip,omitempty"`
	BaseBranch   string `json:"base_branch,omitempty"`
	MergeBase    string `json:"merge_base,omitempty"`
	HasMerged    bool   `json:"has_merged,omitempty"`
	MergedCommit string `json:"merged_commit,omitempty"`
	MergerID     string `json:"merger_id,omitempty"`
	MergedUnix   int64  `json:"merged_unix,omitempty"`
	MaintainerEd bool   `json:"allow_maintainer_edit,omitempty"`
	Flow         string `json:"flow,omitempty"`
	Status       string `json:"status,omitempty"`
	Visible      bool   `json:"visible"`
	HiddenReason string `json:"hidden_reason,omitempty"`
	Complete     bool   `json:"complete"`
}

// SnapshotReview is the permission-checked view of one native review.
type SnapshotReview struct {
	ID            string `json:"id"`
	IssueID       string `json:"issue_id"`
	Type          string `json:"type,omitempty"`
	ReviewerID    string `json:"reviewer_id,omitempty"`
	CommitID      string `json:"commit_id,omitempty"`
	Official      bool   `json:"official,omitempty"`
	Stale         bool   `json:"stale,omitempty"`
	Dismissed     bool   `json:"dismissed,omitempty"`
	ContentDigest string `json:"content_digest,omitempty"`
	CreatedUnix   int64  `json:"created_unix,omitempty"`
	UpdatedUnix   int64  `json:"updated_unix,omitempty"`
	Visible       bool   `json:"visible"`
	HiddenReason  string `json:"hidden_reason,omitempty"`
	Complete      bool   `json:"complete"`
}

// SnapshotReviewPage carries one bounded review list with completeness
// evidence.
type SnapshotReviewPage struct {
	IssueID  string           `json:"issue_id"`
	Items    []SnapshotReview `json:"items"`
	Total    int              `json:"total"`
	Complete bool             `json:"complete"`
}

// SnapshotCheck is the permission-checked view of one native commit status.
type SnapshotCheck struct {
	ID           string `json:"id"`
	Index        int64  `json:"index,omitempty"`
	SHA          string `json:"sha"`
	Context      string `json:"context,omitempty"`
	State        string `json:"state,omitempty"`
	CreatorID    string `json:"creator_id,omitempty"`
	CreatedUnix  int64  `json:"created_unix,omitempty"`
	UpdatedUnix  int64  `json:"updated_unix,omitempty"`
	Visible      bool   `json:"visible"`
	HiddenReason string `json:"hidden_reason,omitempty"`
	Complete     bool   `json:"complete"`
}

// SnapshotCheckSet carries one bounded check list for an exact commit.
type SnapshotCheckSet struct {
	SHA      string          `json:"sha"`
	Items    []SnapshotCheck `json:"items"`
	Total    int             `json:"total"`
	Complete bool            `json:"complete"`
}

// SnapshotRef is the permission-checked view of one native branch tip.
type SnapshotRef struct {
	Ref          string `json:"ref"`
	OID          string `json:"oid,omitempty"`
	Exists       bool   `json:"exists,omitempty"`
	Visible      bool   `json:"visible"`
	HiddenReason string `json:"hidden_reason,omitempty"`
	Complete     bool   `json:"complete"`
}

// NativeSnapshot is one evidence set answering a SnapshotRequest. Only
// requested families are present. The caller binds its bracketed native
// revision; the host never sets it.
type NativeSnapshot struct {
	RepositoryID string                  `json:"repository_id"`
	Issue        *SnapshotIssue          `json:"issue,omitempty"`
	Comments     *SnapshotCommentPage    `json:"comments,omitempty"`
	Dependencies *SnapshotDependencyPage `json:"dependencies,omitempty"`
	Pull         *SnapshotPull           `json:"pull,omitempty"`
	Reviews      *SnapshotReviewPage     `json:"reviews,omitempty"`
	Checks       *SnapshotCheckSet       `json:"checks,omitempty"`
	Refs         []SnapshotRef           `json:"refs,omitempty"`
}
