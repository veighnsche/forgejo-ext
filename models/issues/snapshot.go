// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// Snapshot conversion for authoritative native reads (FT10, early start).
//
// These types convert already-loaded native records into permission-checked
// snapshots carrying exact versions, creation/title/lifecycle evidence, edge
// occurrences, visibility and completeness. They perform no database reads:
// the caller supplies current rows read from live database/ref state (never
// stale replicas or caches) with the actor's visibility decision, and the
// conversion enforces redaction for hidden records.
//
// Hidden snapshots preserve only the structural locator IDs the caller
// already supplied, with Visible=false and a bounded HiddenReason. Text,
// digests, user attribution, versions and lifecycle state are zeroed so an
// inaccessible record leaks nothing. The consumer treats a hidden required
// record as blocking, never as authorization.
//
// Completeness is explicit: single-item snapshots set Complete only when
// every required joined input (history flag, lifecycle events) was read;
// pages set Complete only when all Total items are Returned. A changed/busy
// native revision, missing page or incomplete read cannot authorize work;
// that bracketing is owned by the operation revision observation and the
// Soda caller, not by this conversion.
//
// No factory policy is interpreted here: no acceptance, readiness, budget
// or approval fields. Authoritative guarantees wait for FT09 (complete
// writer domain) plus the FT10 revision-bracket proof; until then these
// snapshots are conversion/DTO work only, not F-read.

// ErrSnapshotInvalid rejects malformed snapshot inputs rather than silently
// broadening or defaulting them.
var ErrSnapshotInvalid = errors.New("invalid native snapshot input")

// Hidden reasons are bounded so callers cannot smuggle native internals.
const (
	HiddenReasonNotFound  = "not_found"
	HiddenReasonNoAccess  = "no_access"
	HiddenReasonRedacted  = "redacted"
	HiddenReasonWithdrawn = "withdrawn"
)

func validHiddenReason(reason string) bool {
	switch reason {
	case HiddenReasonNotFound, HiddenReasonNoAccess, HiddenReasonRedacted, HiddenReasonWithdrawn:
		return true
	default:
		return false
	}
}

// ContentDigest binds exact native text bytes. The digest of empty text is
// the digest of the empty string; hidden text has no digest at all.
func ContentDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Lifecycle event kinds for title/lifecycle evidence.
const (
	LifecycleCreated  = "created"
	LifecycleRetitled = "retitled"
	LifecycleClosed   = "closed"
	LifecycleReopened = "reopened"
	LifecycleEdited   = "edited"
)

func validLifecycleKind(kind string) bool {
	switch kind {
	case LifecycleCreated, LifecycleRetitled, LifecycleClosed, LifecycleReopened, LifecycleEdited:
		return true
	default:
		return false
	}
}

// TitleLifecycleEvent is one title/lifecycle transition with its actor and
// timestamp. OldTitle/NewTitle are set only for retitle events.
type TitleLifecycleEvent struct {
	Kind     string
	AtUnix   int64
	ActorID  int64
	OldTitle string
	NewTitle string
}

func validLifecycleEvent(event TitleLifecycleEvent) bool {
	if !validLifecycleKind(event.Kind) || event.AtUnix <= 0 || event.ActorID < 0 {
		return false
	}
	if event.Kind == LifecycleRetitled {
		return event.OldTitle != event.NewTitle
	}
	return event.OldTitle == "" && event.NewTitle == ""
}

// LifecycleEventFromComment maps title/lifecycle comments to events. It
// reports false for comment types that carry no title/lifecycle transition.
// The caller supplies already-loaded comments; no database access happens.
func LifecycleEventFromComment(comment *Comment) (TitleLifecycleEvent, bool) {
	if comment == nil {
		return TitleLifecycleEvent{}, false
	}
	event := TitleLifecycleEvent{AtUnix: int64(comment.CreatedUnix), ActorID: comment.PosterID}
	switch comment.Type {
	case CommentTypeChangeTitle:
		event.Kind = LifecycleRetitled
		event.OldTitle = comment.OldTitle
		event.NewTitle = comment.NewTitle
	case CommentTypeClose:
		event.Kind = LifecycleClosed
	case CommentTypeReopen:
		event.Kind = LifecycleReopened
	default:
		return TitleLifecycleEvent{}, false
	}
	if !validLifecycleEvent(event) {
		return TitleLifecycleEvent{}, false
	}
	return event, true
}

// CreationProvenance binds verified creation evidence. Verified requires
// visibility plus the loader-observed first-created history flag; cached
// text or author names alone never verify provenance.
type CreationProvenance struct {
	PosterID     int64
	CreatedUnix  int64
	FirstCreated bool
	Verified     bool
}

// IssueSnapshot is the permission-checked view of one native issue.
type IssueSnapshot struct {
	RepositoryID   int64
	IssueID        int64
	Index          int64
	Title          string
	Content        string
	TitleDigest    string
	ContentDigest  string
	ContentVersion int
	NumComments    int
	IsClosed       bool
	IsLocked       bool
	IsPull         bool
	Provenance     CreationProvenance
	Lifecycle      []TitleLifecycleEvent
	CreatedUnix    int64
	UpdatedUnix    int64
	ClosedUnix     int64
	Visible        bool
	HiddenReason   string
	Complete       bool
}

// NewIssueSnapshot converts one current issue row. historyFirstCreated is
// the loader-observed ContentHistory first-created flag for the issue body.
// lifecycle holds the loader-read title/lifecycle transitions in time order.
// When visible is false the issue is redacted to its locator IDs.
func NewIssueSnapshot(issue *Issue, historyFirstCreated bool, lifecycle []TitleLifecycleEvent, visible bool, hiddenReason string, complete bool) (IssueSnapshot, error) {
	if issue == nil || issue.ID <= 0 || issue.RepoID <= 0 || issue.Index <= 0 {
		return IssueSnapshot{}, ErrSnapshotInvalid
	}
	if !visible {
		if !validHiddenReason(hiddenReason) {
			return IssueSnapshot{}, ErrSnapshotInvalid
		}
		return IssueSnapshot{
			RepositoryID: issue.RepoID,
			IssueID:      issue.ID,
			Index:        issue.Index,
			Visible:      false,
			HiddenReason: hiddenReason,
			Complete:     complete,
		}, nil
	}
	if hiddenReason != "" {
		return IssueSnapshot{}, ErrSnapshotInvalid
	}
	if issue.ContentVersion < 0 || issue.NumComments < 0 {
		return IssueSnapshot{}, ErrSnapshotInvalid
	}
	created := int64(issue.CreatedUnix)
	updated := int64(issue.UpdatedUnix)
	closed := int64(issue.ClosedUnix)
	if created <= 0 || updated < created {
		return IssueSnapshot{}, ErrSnapshotInvalid
	}
	if issue.IsClosed != (closed > 0) {
		return IssueSnapshot{}, ErrSnapshotInvalid
	}
	if closed > 0 && closed < created {
		return IssueSnapshot{}, ErrSnapshotInvalid
	}
	for _, event := range lifecycle {
		if !validLifecycleEvent(event) {
			return IssueSnapshot{}, ErrSnapshotInvalid
		}
	}
	return IssueSnapshot{
		RepositoryID:   issue.RepoID,
		IssueID:        issue.ID,
		Index:          issue.Index,
		Title:          issue.Title,
		Content:        issue.Content,
		TitleDigest:    ContentDigest(issue.Title),
		ContentDigest:  ContentDigest(issue.Content),
		ContentVersion: issue.ContentVersion,
		NumComments:    issue.NumComments,
		IsClosed:       issue.IsClosed,
		IsLocked:       issue.IsLocked,
		IsPull:         issue.IsPull,
		Provenance: CreationProvenance{
			PosterID:     issue.PosterID,
			CreatedUnix:  created,
			FirstCreated: historyFirstCreated,
			Verified:     historyFirstCreated && issue.PosterID > 0,
		},
		Lifecycle:   append([]TitleLifecycleEvent(nil), lifecycle...),
		CreatedUnix: created,
		UpdatedUnix: updated,
		ClosedUnix:  closed,
		Visible:     true,
		Complete:    complete,
	}, nil
}

// CommentSnapshot is the permission-checked view of one native comment.
type CommentSnapshot struct {
	CommentID      int64
	IssueID        int64
	Type           string
	PosterID       int64
	Content        string
	ContentDigest  string
	ContentVersion int
	ReviewID       int64
	Invalidated    bool
	CreatedUnix    int64
	UpdatedUnix    int64
	Visible        bool
	HiddenReason   string
	Complete       bool
}

// NewCommentSnapshot converts one current comment row. When visible is
// false the comment is redacted to its locator IDs.
func NewCommentSnapshot(comment *Comment, visible bool, hiddenReason string, complete bool) (CommentSnapshot, error) {
	if comment == nil || comment.ID <= 0 || comment.IssueID <= 0 {
		return CommentSnapshot{}, ErrSnapshotInvalid
	}
	if !visible {
		if !validHiddenReason(hiddenReason) {
			return CommentSnapshot{}, ErrSnapshotInvalid
		}
		return CommentSnapshot{
			CommentID:    comment.ID,
			IssueID:      comment.IssueID,
			Visible:      false,
			HiddenReason: hiddenReason,
			Complete:     complete,
		}, nil
	}
	if hiddenReason != "" {
		return CommentSnapshot{}, ErrSnapshotInvalid
	}
	if comment.ContentVersion < 0 || comment.PosterID < 0 || comment.ReviewID < 0 {
		return CommentSnapshot{}, ErrSnapshotInvalid
	}
	created := int64(comment.CreatedUnix)
	updated := int64(comment.UpdatedUnix)
	if created <= 0 || updated < created {
		return CommentSnapshot{}, ErrSnapshotInvalid
	}
	commentType := ""
	if comment.Type >= 0 {
		commentType = comment.Type.String()
	}
	if commentType == "" {
		return CommentSnapshot{}, ErrSnapshotInvalid
	}
	return CommentSnapshot{
		CommentID:      comment.ID,
		IssueID:        comment.IssueID,
		Type:           commentType,
		PosterID:       comment.PosterID,
		Content:        comment.Content,
		ContentDigest:  ContentDigest(comment.Content),
		ContentVersion: comment.ContentVersion,
		ReviewID:       comment.ReviewID,
		Invalidated:    comment.Invalidated,
		CreatedUnix:    created,
		UpdatedUnix:    updated,
		Visible:        true,
		Complete:       complete,
	}, nil
}

// CommentPage carries one bounded comment list with completeness evidence.
type CommentPage struct {
	IssueID  int64
	Items    []CommentSnapshot
	Total    int
	Complete bool
}

// NewCommentPage validates one loader-read comment list. Complete requires
// every Total item Returned with each item complete.
func NewCommentPage(issueID int64, items []CommentSnapshot, total int, complete bool) (CommentPage, error) {
	if issueID <= 0 || total < 0 || len(items) > total {
		return CommentPage{}, ErrSnapshotInvalid
	}
	for _, item := range items {
		if item.IssueID != issueID || !item.Complete {
			return CommentPage{}, ErrSnapshotInvalid
		}
	}
	if complete && len(items) != total {
		return CommentPage{}, ErrSnapshotInvalid
	}
	return CommentPage{
		IssueID:  issueID,
		Items:    append([]CommentSnapshot(nil), items...),
		Total:    total,
		Complete: complete,
	}, nil
}

// DependencySnapshot is one edge occurrence. OccurrenceID is the native
// dependency row ID: removal followed by readdition yields a new row, so
// consumers detect replacement by ID change, never by text comparison.
// A hidden occurrence preserves only the source-side locators; the target
// DependencyID is redacted so an inaccessible prerequisite blocks work
// without leaking which issue it is.
type DependencySnapshot struct {
	OccurrenceID int64
	IssueID      int64
	DependencyID int64
	CreatedUnix  int64
	UpdatedUnix  int64
	Visible      bool
	HiddenReason string
	Complete     bool
}

// NewDependencySnapshot converts one current dependency row.
func NewDependencySnapshot(dep *IssueDependency, visible bool, hiddenReason string, complete bool) (DependencySnapshot, error) {
	if dep == nil || dep.ID <= 0 || dep.IssueID <= 0 || dep.DependencyID <= 0 {
		return DependencySnapshot{}, ErrSnapshotInvalid
	}
	if !visible {
		if !validHiddenReason(hiddenReason) {
			return DependencySnapshot{}, ErrSnapshotInvalid
		}
		return DependencySnapshot{
			OccurrenceID: dep.ID,
			IssueID:      dep.IssueID,
			Visible:      false,
			HiddenReason: hiddenReason,
			Complete:     complete,
		}, nil
	}
	if hiddenReason != "" {
		return DependencySnapshot{}, ErrSnapshotInvalid
	}
	created := int64(dep.CreatedUnix)
	updated := int64(dep.UpdatedUnix)
	if created <= 0 || updated < created {
		return DependencySnapshot{}, ErrSnapshotInvalid
	}
	return DependencySnapshot{
		OccurrenceID: dep.ID,
		IssueID:      dep.IssueID,
		DependencyID: dep.DependencyID,
		CreatedUnix:  created,
		UpdatedUnix:  updated,
		Visible:      true,
		Complete:     complete,
	}, nil
}

// DependencyPage carries one bounded edge-occurrence list with completeness.
type DependencyPage struct {
	IssueID  int64
	Items    []DependencySnapshot
	Total    int
	Complete bool
}

// NewDependencyPage validates one loader-read dependency list.
func NewDependencyPage(issueID int64, items []DependencySnapshot, total int, complete bool) (DependencyPage, error) {
	if issueID <= 0 || total < 0 || len(items) > total {
		return DependencyPage{}, ErrSnapshotInvalid
	}
	for _, item := range items {
		if item.IssueID != issueID || !item.Complete {
			return DependencyPage{}, ErrSnapshotInvalid
		}
	}
	if complete && len(items) != total {
		return DependencyPage{}, ErrSnapshotInvalid
	}
	return DependencyPage{
		IssueID:  issueID,
		Items:    append([]DependencySnapshot(nil), items...),
		Total:    total,
		Complete: complete,
	}, nil
}

func validFullBranchRef(ref string) bool {
	if !strings.HasPrefix(ref, "refs/heads/") || len(ref) > 512 {
		return false
	}
	name := strings.TrimSpace(ref)
	if name != ref || strings.ContainsAny(ref, " ~^:?*\\") || strings.Contains(ref, "..") {
		return false
	}
	return len(strings.TrimPrefix(ref, "refs/heads/")) > 0
}

func validFullOID(oid string) bool {
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

// PullSnapshot is the permission-checked view of one native pull request.
// HeadTip is the loader-resolved current head tip from live ref state;
// empty is allowed only for merged PRs whose branch is gone.
type PullSnapshot struct {
	PullID            int64
	IssueID           int64
	Index             int64
	HeadRepositoryID  int64
	HeadBranch        string
	HeadTip           string
	BaseBranch        string
	MergeBase         string
	HasMerged         bool
	MergedCommitID    string
	MergerID          int64
	MergedUnix        int64
	AllowMaintainerEd bool
	Flow              string
	Status            string
	Visible           bool
	HiddenReason      string
	Complete          bool
}

// NewPullSnapshot converts one current pull-request row with its head tip.
func NewPullSnapshot(pr *PullRequest, headTip string, visible bool, hiddenReason string, complete bool) (PullSnapshot, error) {
	if pr == nil || pr.ID <= 0 || pr.IssueID <= 0 || pr.Index <= 0 {
		return PullSnapshot{}, ErrSnapshotInvalid
	}
	if !visible {
		if !validHiddenReason(hiddenReason) {
			return PullSnapshot{}, ErrSnapshotInvalid
		}
		return PullSnapshot{
			PullID:       pr.ID,
			IssueID:      pr.IssueID,
			Index:        pr.Index,
			Visible:      false,
			HiddenReason: hiddenReason,
			Complete:     complete,
		}, nil
	}
	if hiddenReason != "" {
		return PullSnapshot{}, ErrSnapshotInvalid
	}
	if pr.HeadRepoID <= 0 || pr.BaseRepoID <= 0 || pr.HeadBranch == "" || pr.BaseBranch == "" {
		return PullSnapshot{}, ErrSnapshotInvalid
	}
	if headTip != "" && !validFullOID(headTip) {
		return PullSnapshot{}, ErrSnapshotInvalid
	}
	if !pr.HasMerged && headTip == "" {
		return PullSnapshot{}, ErrSnapshotInvalid
	}
	if pr.MergeBase != "" && !validFullOID(pr.MergeBase) {
		return PullSnapshot{}, ErrSnapshotInvalid
	}
	if pr.MergedCommitID != "" && !validFullOID(pr.MergedCommitID) {
		return PullSnapshot{}, ErrSnapshotInvalid
	}
	merged := int64(pr.MergedUnix)
	if pr.HasMerged != (merged > 0 || pr.MergedCommitID != "") {
		return PullSnapshot{}, ErrSnapshotInvalid
	}
	flow := ""
	switch pr.Flow {
	case PullRequestFlowGithub:
		flow = "github"
	case PullRequestFlowAGit:
		flow = "agit"
	default:
		return PullSnapshot{}, ErrSnapshotInvalid
	}
	return PullSnapshot{
		PullID:            pr.ID,
		IssueID:           pr.IssueID,
		Index:             pr.Index,
		HeadRepositoryID:  pr.HeadRepoID,
		HeadBranch:        pr.HeadBranch,
		HeadTip:           strings.ToLower(headTip),
		BaseBranch:        pr.BaseBranch,
		MergeBase:         strings.ToLower(pr.MergeBase),
		HasMerged:         pr.HasMerged,
		MergedCommitID:    strings.ToLower(pr.MergedCommitID),
		MergerID:          pr.MergerID,
		MergedUnix:        merged,
		AllowMaintainerEd: pr.AllowMaintainerEdit,
		Flow:              flow,
		Status:            pr.Status.String(),
		Visible:           true,
		Complete:          complete,
	}, nil
}

// ReviewSnapshot is the permission-checked view of one native review.
type ReviewSnapshot struct {
	ReviewID      int64
	IssueID       int64
	Type          string
	ReviewerID    int64
	CommitID      string
	Official      bool
	Stale         bool
	Dismissed     bool
	ContentDigest string
	CreatedUnix   int64
	UpdatedUnix   int64
	Visible       bool
	HiddenReason  string
	Complete      bool
}

func reviewTypeName(reviewType ReviewType) string {
	switch reviewType {
	case ReviewTypePending:
		return "PENDING"
	case ReviewTypeApprove:
		return "APPROVED"
	case ReviewTypeComment:
		return "COMMENT"
	case ReviewTypeReject:
		return "REQUEST_CHANGES"
	case ReviewTypeRequest:
		return "REQUEST_REVIEW"
	default:
		return ""
	}
}

// NewReviewSnapshot converts one current review row.
func NewReviewSnapshot(review *Review, visible bool, hiddenReason string, complete bool) (ReviewSnapshot, error) {
	if review == nil || review.ID <= 0 || review.IssueID <= 0 {
		return ReviewSnapshot{}, ErrSnapshotInvalid
	}
	if !visible {
		if !validHiddenReason(hiddenReason) {
			return ReviewSnapshot{}, ErrSnapshotInvalid
		}
		return ReviewSnapshot{
			ReviewID:     review.ID,
			IssueID:      review.IssueID,
			Visible:      false,
			HiddenReason: hiddenReason,
			Complete:     complete,
		}, nil
	}
	if hiddenReason != "" {
		return ReviewSnapshot{}, ErrSnapshotInvalid
	}
	typeName := reviewTypeName(review.Type)
	if typeName == "" || review.ReviewerID < 0 {
		return ReviewSnapshot{}, ErrSnapshotInvalid
	}
	if !validFullOID(review.CommitID) {
		return ReviewSnapshot{}, ErrSnapshotInvalid
	}
	created := int64(review.CreatedUnix)
	updated := int64(review.UpdatedUnix)
	if created <= 0 || updated < created {
		return ReviewSnapshot{}, ErrSnapshotInvalid
	}
	return ReviewSnapshot{
		ReviewID:      review.ID,
		IssueID:       review.IssueID,
		Type:          typeName,
		ReviewerID:    review.ReviewerID,
		CommitID:      strings.ToLower(review.CommitID),
		Official:      review.Official,
		Stale:         review.Stale,
		Dismissed:     review.Dismissed,
		ContentDigest: ContentDigest(review.Content),
		CreatedUnix:   created,
		UpdatedUnix:   updated,
		Visible:       true,
		Complete:      complete,
	}, nil
}

// ReviewPage carries one bounded review list with completeness evidence.
type ReviewPage struct {
	IssueID  int64
	Items    []ReviewSnapshot
	Total    int
	Complete bool
}

// NewReviewPage validates one loader-read review list.
func NewReviewPage(issueID int64, items []ReviewSnapshot, total int, complete bool) (ReviewPage, error) {
	if issueID <= 0 || total < 0 || len(items) > total {
		return ReviewPage{}, ErrSnapshotInvalid
	}
	for _, item := range items {
		if item.IssueID != issueID || !item.Complete {
			return ReviewPage{}, ErrSnapshotInvalid
		}
	}
	if complete && len(items) != total {
		return ReviewPage{}, ErrSnapshotInvalid
	}
	return ReviewPage{
		IssueID:  issueID,
		Items:    append([]ReviewSnapshot(nil), items...),
		Total:    total,
		Complete: complete,
	}, nil
}
