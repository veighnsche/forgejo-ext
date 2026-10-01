// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"errors"
	"os"
	"sort"
	"strconv"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
)

// Permission-checked native snapshot reads (FT10, F-read).
//
// ReadSnapshot loads current native rows from live database/ref state and
// converts them through the snapshot DTOs with the actor's visibility
// decision. It claims no reservation and performs no mutation: reads never
// contend with writers, and the caller brackets the read between two equal
// idle ReadNativeRevision observations instead.
//
// Visibility rules: the dispatcher already verified the actor holds read
// access to the repository. Same-repository records additionally require
// their unit (issues/pulls for collaboration, code for checks/refs).
// Cross-repository dependency targets are checked against the target
// repository and redacted when inaccessible. Missing records and gated
// singles report ErrSnapshotNotFound identically, so the response leaks no
// existence distinction; list members that are individually hidden keep
// their redacted envelope so the consumer blocks on the exact occurrence.
//
// Over-limit lists report Complete=false rather than a silent prefix: the
// consumer must refuse partial evidence. Cursor paging selects items with
// IDs strictly greater than the decimal cursor, ordered ascending.

var (
	// ErrSnapshotInvalid rejects malformed snapshot requests.
	ErrSnapshotInvalid = errors.New("invalid native snapshot request")
	// ErrSnapshotNotFound reports a missing or inaccessible snapshot record.
	// Gated and missing singles share this error so callers cannot probe
	// existence.
	ErrSnapshotNotFound = errors.New("native snapshot record not found")
	// ErrSnapshotUnavailable reports a native read failure (database or
	// repository access) that neither proves nor denies the records.
	ErrSnapshotUnavailable = errors.New("native snapshot read unavailable")
)

const (
	// snapshotFetchCap bounds one loader fetch above the wire page limit
	// so overflow is detected rather than silently truncated.
	snapshotFetchCap = 501
	// snapshotLifecycleCap bounds title/lifecycle transitions per comment
	// kind; overflow marks the issue incomplete.
	snapshotLifecycleCap = 64
)

// ReadSnapshot loads one permission-checked native snapshot for a verified
// read actor. Only requested families are present. It performs no mutation
// and claims no reservation.
func (s *Service) ReadSnapshot(ctx context.Context, actorID int64, req sdk.SnapshotRequest) (sdk.NativeSnapshot, error) {
	if err := sdk.ValidateSnapshotRequest(req); err != nil {
		return sdk.NativeSnapshot{}, ErrSnapshotInvalid
	}
	if actorID <= 0 {
		return sdk.NativeSnapshot{}, ErrSnapshotInvalid
	}
	repositoryID, _ := strconv.ParseInt(req.RepositoryID, 10, 64)
	requestActorID, _ := strconv.ParseInt(req.ActorID, 10, 64)
	if requestActorID != actorID {
		return sdk.NativeSnapshot{}, ErrSnapshotInvalid
	}
	repository, err := repo_model.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return sdk.NativeSnapshot{}, ErrSnapshotInvalid
		}
		return sdk.NativeSnapshot{}, ErrSnapshotUnavailable
	}
	user, err := user_model.GetUserByID(ctx, actorID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return sdk.NativeSnapshot{}, ErrSnapshotInvalid
		}
		return sdk.NativeSnapshot{}, ErrSnapshotUnavailable
	}
	permission, err := access_model.GetUserRepoPermission(ctx, repository, user)
	if err != nil {
		return sdk.NativeSnapshot{}, ErrSnapshotUnavailable
	}
	loader := &snapshotLoader{
		service:    s,
		repository: repository,
		user:       user,
		permission: permission,
		limit:      req.Limit,
		cursor:     req.Cursor,
	}
	if loader.limit <= 0 {
		loader.limit = sdk.SnapshotPageLimit
	}
	snapshot := sdk.NativeSnapshot{RepositoryID: req.RepositoryID}
	for _, family := range req.Families {
		switch family {
		case sdk.SnapshotFamilyIssue:
			issue, err := loader.issue(ctx, req.IssueIndex)
			if err != nil {
				return sdk.NativeSnapshot{}, err
			}
			snapshot.Issue = issue
		case sdk.SnapshotFamilyComments:
			page, err := loader.comments(ctx, req.IssueIndex, req.CommentIDs)
			if err != nil {
				return sdk.NativeSnapshot{}, err
			}
			snapshot.Comments = page
		case sdk.SnapshotFamilyDependencies:
			page, err := loader.dependencies(ctx, req.IssueIndex)
			if err != nil {
				return sdk.NativeSnapshot{}, err
			}
			snapshot.Dependencies = page
		case sdk.SnapshotFamilyPull:
			pull, err := loader.pull(ctx, req.PullNumber)
			if err != nil {
				return sdk.NativeSnapshot{}, err
			}
			snapshot.Pull = pull
		case sdk.SnapshotFamilyReviews:
			page, err := loader.reviews(ctx, req.IssueIndex, req.PullNumber)
			if err != nil {
				return sdk.NativeSnapshot{}, err
			}
			snapshot.Reviews = page
		case sdk.SnapshotFamilyChecks:
			set, err := loader.checks(ctx, req.SHA)
			if err != nil {
				return sdk.NativeSnapshot{}, err
			}
			snapshot.Checks = set
		case sdk.SnapshotFamilyRefs:
			refs, err := loader.refs(ctx, req.Refs)
			if err != nil {
				return sdk.NativeSnapshot{}, err
			}
			snapshot.Refs = refs
		default:
			return sdk.NativeSnapshot{}, ErrSnapshotInvalid
		}
	}
	return snapshot, nil
}

type snapshotLoader struct {
	service    *Service
	repository *repo_model.Repository
	user       *user_model.User
	permission access_model.Permission
	limit      int
	cursor     string
}

func snapshotIDString(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// cursorAfterID parses the decimal after-ID cursor. Empty means from the
// start; the request validator already rejected non-decimal cursors.
func (l *snapshotLoader) cursorAfterID() int64 {
	if l.cursor == "" {
		return 0
	}
	after, _ := strconv.ParseInt(l.cursor, 10, 64)
	return after
}

// loadIssue resolves one repository issue by index with its unit check.
// Missing and gated issues report ErrSnapshotNotFound identically.
func (l *snapshotLoader) loadIssue(ctx context.Context, index string) (*issues_model.Issue, error) {
	number, _ := strconv.ParseInt(index, 10, 64)
	issue, err := issues_model.GetIssueByIndex(ctx, l.repository.ID, number)
	if err != nil {
		if issues_model.IsErrIssueNotExist(err) {
			return nil, ErrSnapshotNotFound
		}
		return nil, ErrSnapshotUnavailable
	}
	if !l.permission.CanReadIssuesOrPulls(issue.IsPull) {
		return nil, ErrSnapshotNotFound
	}
	return issue, nil
}

func (l *snapshotLoader) issue(ctx context.Context, index string) (*sdk.SnapshotIssue, error) {
	issue, err := l.loadIssue(ctx, index)
	if err != nil {
		return nil, err
	}
	firstCreated, err := l.issueFirstCreated(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	lifecycle, complete, err := l.issueLifecycle(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	converted, err := issues_model.NewIssueSnapshot(issue, firstCreated, lifecycle, true, "", complete)
	if err != nil {
		return nil, ErrSnapshotUnavailable
	}
	out := &sdk.SnapshotIssue{
		ID:             snapshotIDString(converted.IssueID),
		Index:          snapshotIDString(converted.Index),
		Title:          converted.Title,
		Content:        converted.Content,
		TitleDigest:    converted.TitleDigest,
		ContentDigest:  converted.ContentDigest,
		ContentVersion: converted.ContentVersion,
		NumComments:    converted.NumComments,
		IsClosed:       converted.IsClosed,
		IsLocked:       converted.IsLocked,
		IsPull:         converted.IsPull,
		CreatedUnix:    converted.CreatedUnix,
		UpdatedUnix:    converted.UpdatedUnix,
		ClosedUnix:     converted.ClosedUnix,
		Visible:        true,
		Complete:       converted.Complete,
	}
	out.Provenance = sdk.SnapshotCreationProvenance{
		PosterID:     snapshotIDString(converted.Provenance.PosterID),
		CreatedUnix:  converted.Provenance.CreatedUnix,
		FirstCreated: converted.Provenance.FirstCreated,
		Verified:     converted.Provenance.Verified,
	}
	for _, event := range converted.Lifecycle {
		out.Lifecycle = append(out.Lifecycle, sdk.SnapshotLifecycleEvent{
			Kind:     event.Kind,
			AtUnix:   event.AtUnix,
			ActorID:  snapshotIDString(event.ActorID),
			OldTitle: event.OldTitle,
			NewTitle: event.NewTitle,
		})
	}
	return out, nil
}

// issueFirstCreated reports whether the loader observed the issue body's
// first-created history row. Cached text alone never verifies provenance.
func (l *snapshotLoader) issueFirstCreated(ctx context.Context, issueID int64) (bool, error) {
	// Issue body history uses comment ID zero; comments use their own ID.
	history, err := issues_model.FetchIssueContentHistoryList(ctx, issueID, 0)
	if err != nil {
		return false, ErrSnapshotUnavailable
	}
	for _, item := range history {
		if item.IsFirstCreated && !item.IsDeleted {
			return true, nil
		}
	}
	return false, nil
}

// issueLifecycle reads the title/lifecycle transitions in comment order.
// Overflow beyond the per-kind cap marks the issue incomplete rather than
// silently dropping transitions.
func (l *snapshotLoader) issueLifecycle(ctx context.Context, issueID int64) ([]issues_model.TitleLifecycleEvent, bool, error) {
	type orderedEvent struct {
		commentID int64
		event     issues_model.TitleLifecycleEvent
	}
	var ordered []orderedEvent
	complete := true
	for _, kind := range []issues_model.CommentType{
		issues_model.CommentTypeChangeTitle,
		issues_model.CommentTypeClose,
		issues_model.CommentTypeReopen,
	} {
		comments, err := issues_model.FindComments(ctx, &issues_model.FindCommentsOptions{
			ListOptions: db.ListOptions{Page: 1, PageSize: snapshotLifecycleCap + 1},
			IssueID:     issueID,
			Type:        kind,
		})
		if err != nil {
			return nil, false, ErrSnapshotUnavailable
		}
		if len(comments) > snapshotLifecycleCap {
			complete = false
			comments = comments[:snapshotLifecycleCap]
		}
		for _, comment := range comments {
			event, ok := issues_model.LifecycleEventFromComment(comment)
			if !ok {
				continue
			}
			ordered = append(ordered, orderedEvent{commentID: comment.ID, event: event})
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].commentID < ordered[j].commentID })
	lifecycle := make([]issues_model.TitleLifecycleEvent, 0, len(ordered))
	for _, item := range ordered {
		lifecycle = append(lifecycle, item.event)
	}
	return lifecycle, complete, nil
}

func (l *snapshotLoader) comments(ctx context.Context, index string, ids []string) (*sdk.SnapshotCommentPage, error) {
	if len(ids) > 0 {
		return l.commentsByIDs(ctx, ids)
	}
	issue, err := l.loadIssue(ctx, index)
	if err != nil {
		return nil, err
	}
	total, err := issues_model.CountComments(ctx, &issues_model.FindCommentsOptions{IssueID: issue.ID})
	if err != nil {
		return nil, ErrSnapshotUnavailable
	}
	rows, err := issues_model.FindComments(ctx, &issues_model.FindCommentsOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: snapshotFetchCap},
		IssueID:     issue.ID,
	})
	if err != nil {
		return nil, ErrSnapshotUnavailable
	}
	after := l.cursorAfterID()
	var kept []*issues_model.Comment
	remaining := 0
	for _, row := range rows {
		if row.ID <= after {
			continue
		}
		remaining++
	}
	capped := int64(len(rows)) >= snapshotFetchCap && total > int64(len(rows))
	fetchedComplete := !capped
	for _, row := range rows {
		if row.ID <= after {
			continue
		}
		if len(kept) >= l.limit {
			break
		}
		kept = append(kept, row)
	}
	page := &sdk.SnapshotCommentPage{IssueID: snapshotIDString(issue.ID), Total: remaining}
	for _, row := range kept {
		converted, err := issues_model.NewCommentSnapshot(row, true, "", true)
		if err != nil {
			return nil, ErrSnapshotUnavailable
		}
		page.Items = append(page.Items, sdk.SnapshotComment{
			ID:             snapshotIDString(converted.CommentID),
			IssueID:        snapshotIDString(converted.IssueID),
			Type:           converted.Type,
			PosterID:       snapshotIDString(converted.PosterID),
			Content:        converted.Content,
			ContentDigest:  converted.ContentDigest,
			ContentVersion: converted.ContentVersion,
			ReviewID:       snapshotIDString(converted.ReviewID),
			Invalidated:    converted.Invalidated,
			CreatedUnix:    converted.CreatedUnix,
			UpdatedUnix:    converted.UpdatedUnix,
			Visible:        true,
			Complete:       true,
		})
	}
	// Total counts remaining items after the cursor only when the fetch
	// covered them; otherwise the page is honestly incomplete.
	if fetchedComplete {
		page.Complete = len(kept) == remaining
	}
	return page, nil
}

// commentsByIDs converts explicitly selected comments. Every ID must resolve
// to a comment on one issue in this repository; missing, foreign or gated
// IDs fail the read rather than leaking or guessing.
func (l *snapshotLoader) commentsByIDs(ctx context.Context, ids []string) (*sdk.SnapshotCommentPage, error) {
	var issueID int64
	page := &sdk.SnapshotCommentPage{}
	for _, id := range ids {
		number, _ := strconv.ParseInt(id, 10, 64)
		comment, err := issues_model.GetCommentByID(ctx, number)
		if err != nil {
			if issues_model.IsErrCommentNotExist(err) {
				return nil, ErrSnapshotNotFound
			}
			return nil, ErrSnapshotUnavailable
		}
		issue, err := issues_model.GetIssueByID(ctx, comment.IssueID)
		if err != nil {
			if issues_model.IsErrIssueNotExist(err) {
				return nil, ErrSnapshotNotFound
			}
			return nil, ErrSnapshotUnavailable
		}
		if issue.RepoID != l.repository.ID || !l.permission.CanReadIssuesOrPulls(issue.IsPull) {
			return nil, ErrSnapshotNotFound
		}
		if issueID == 0 {
			issueID = issue.ID
		} else if issueID != issue.ID {
			return nil, ErrSnapshotInvalid
		}
		converted, err := issues_model.NewCommentSnapshot(comment, true, "", true)
		if err != nil {
			return nil, ErrSnapshotUnavailable
		}
		page.Items = append(page.Items, sdk.SnapshotComment{
			ID:             snapshotIDString(converted.CommentID),
			IssueID:        snapshotIDString(converted.IssueID),
			Type:           converted.Type,
			PosterID:       snapshotIDString(converted.PosterID),
			Content:        converted.Content,
			ContentDigest:  converted.ContentDigest,
			ContentVersion: converted.ContentVersion,
			ReviewID:       snapshotIDString(converted.ReviewID),
			Invalidated:    converted.Invalidated,
			CreatedUnix:    converted.CreatedUnix,
			UpdatedUnix:    converted.UpdatedUnix,
			Visible:        true,
			Complete:       true,
		})
	}
	page.IssueID = snapshotIDString(issueID)
	page.Total = len(page.Items)
	page.Complete = true
	return page, nil
}

func (l *snapshotLoader) dependencies(ctx context.Context, index string) (*sdk.SnapshotDependencyPage, error) {
	issue, err := l.loadIssue(ctx, index)
	if err != nil {
		return nil, err
	}
	rows, err := issues_model.ListIssueDependencies(ctx, issue.ID)
	if err != nil {
		return nil, ErrSnapshotUnavailable
	}
	after := l.cursorAfterID()
	var kept []*issues_model.IssueDependency
	remaining := 0
	for _, row := range rows {
		if row.ID <= after {
			continue
		}
		remaining++
		if len(kept) < l.limit {
			kept = append(kept, row)
		}
	}
	page := &sdk.SnapshotDependencyPage{IssueID: snapshotIDString(issue.ID), Total: remaining}
	for _, row := range kept {
		visible, reason, err := l.dependencyVisible(ctx, row.DependencyID)
		if err != nil {
			return nil, err
		}
		converted, err := issues_model.NewDependencySnapshot(row, visible, reason, true)
		if err != nil {
			return nil, ErrSnapshotUnavailable
		}
		item := sdk.SnapshotDependency{
			OccurrenceID: snapshotIDString(converted.OccurrenceID),
			IssueID:      snapshotIDString(converted.IssueID),
			DependencyID: snapshotIDString(converted.DependencyID),
			CreatedUnix:  converted.CreatedUnix,
			UpdatedUnix:  converted.UpdatedUnix,
			Visible:      converted.Visible,
			HiddenReason: converted.HiddenReason,
			Complete:     true,
		}
		page.Items = append(page.Items, item)
	}
	page.Complete = len(kept) == remaining
	return page, nil
}

// dependencyVisible checks the edge target. Same-repository targets are
// visible; cross-repository targets require read access to their own
// repository and unit, else the edge is redacted without leaking the target.
func (l *snapshotLoader) dependencyVisible(ctx context.Context, targetID int64) (bool, string, error) {
	target, err := issues_model.GetIssueByID(ctx, targetID)
	if err != nil {
		if issues_model.IsErrIssueNotExist(err) {
			return false, issues_model.HiddenReasonNotFound, nil
		}
		return false, "", ErrSnapshotUnavailable
	}
	if target.RepoID == l.repository.ID {
		return true, "", nil
	}
	targetRepo, err := repo_model.GetRepositoryByID(ctx, target.RepoID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return false, issues_model.HiddenReasonNotFound, nil
		}
		return false, "", ErrSnapshotUnavailable
	}
	permission, err := access_model.GetUserRepoPermission(ctx, targetRepo, l.user)
	if err != nil {
		return false, "", ErrSnapshotUnavailable
	}
	if !permission.CanReadIssuesOrPulls(target.IsPull) {
		return false, issues_model.HiddenReasonNoAccess, nil
	}
	return true, "", nil
}

func (l *snapshotLoader) pull(ctx context.Context, number string) (*sdk.SnapshotPull, error) {
	prNumber, _ := strconv.ParseInt(number, 10, 64)
	pr, err := issues_model.GetPullRequestByIndex(ctx, l.repository.ID, prNumber)
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			return nil, ErrSnapshotNotFound
		}
		return nil, ErrSnapshotUnavailable
	}
	if !l.permission.CanRead(unit.TypePullRequests) {
		return nil, ErrSnapshotNotFound
	}
	headTip, ok, err := l.pullHeadTip(ctx, pr)
	if err != nil {
		return nil, err
	}
	if !ok {
		// The pull request is readable but its head tip cannot be
		// resolved from live ref state (deleted fork or branch). Report
		// it hidden so consumers block rather than bind a guessed OID.
		converted, err := issues_model.NewPullSnapshot(pr, "", false, issues_model.HiddenReasonNoAccess, true)
		if err != nil {
			return nil, ErrSnapshotUnavailable
		}
		return &sdk.SnapshotPull{
			ID:           snapshotIDString(converted.PullID),
			IssueID:      snapshotIDString(converted.IssueID),
			Number:       snapshotIDString(converted.Index),
			Visible:      false,
			HiddenReason: converted.HiddenReason,
			Complete:     true,
		}, nil
	}
	converted, err := issues_model.NewPullSnapshot(pr, headTip, true, "", true)
	if err != nil {
		return nil, ErrSnapshotUnavailable
	}
	return &sdk.SnapshotPull{
		ID:           snapshotIDString(converted.PullID),
		IssueID:      snapshotIDString(converted.IssueID),
		Number:       snapshotIDString(converted.Index),
		HeadRepoID:   snapshotIDString(converted.HeadRepositoryID),
		HeadBranch:   converted.HeadBranch,
		HeadTip:      converted.HeadTip,
		BaseBranch:   converted.BaseBranch,
		MergeBase:    converted.MergeBase,
		HasMerged:    converted.HasMerged,
		MergedCommit: converted.MergedCommitID,
		MergerID:     snapshotIDString(converted.MergerID),
		MergedUnix:   converted.MergedUnix,
		MaintainerEd: converted.AllowMaintainerEd,
		Flow:         converted.Flow,
		Status:       converted.Status,
		Visible:      true,
		Complete:     true,
	}, nil
}

// pullHeadTip resolves the current head tip from live ref state. Merged
// pulls tolerate a gone branch; open pulls with an unresolvable head report
// ok=false so the caller hides the pull instead of binding a stale OID.
func (l *snapshotLoader) pullHeadTip(ctx context.Context, pr *issues_model.PullRequest) (string, bool, error) {
	headRepo, err := repo_model.GetRepositoryByID(ctx, pr.HeadRepoID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return "", pr.HasMerged, nil
		}
		return "", false, ErrSnapshotUnavailable
	}
	tip, err := l.service.readRef(ctx, headRepo.RepoPath(), git.BranchPrefix+pr.HeadBranch)
	if err != nil {
		if git.IsErrNotExist(err) {
			return "", pr.HasMerged, nil
		}
		return "", false, ErrSnapshotUnavailable
	}
	return tip, true, nil
}

func (l *snapshotLoader) reviews(ctx context.Context, index, number string) (*sdk.SnapshotReviewPage, error) {
	var issueID int64
	if index != "" {
		issue, err := l.loadIssue(ctx, index)
		if err != nil {
			return nil, err
		}
		if !l.permission.CanRead(unit.TypePullRequests) {
			return nil, ErrSnapshotNotFound
		}
		issueID = issue.ID
	} else {
		prNumber, _ := strconv.ParseInt(number, 10, 64)
		pr, err := issues_model.GetPullRequestByIndex(ctx, l.repository.ID, prNumber)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				return nil, ErrSnapshotNotFound
			}
			return nil, ErrSnapshotUnavailable
		}
		if !l.permission.CanRead(unit.TypePullRequests) {
			return nil, ErrSnapshotNotFound
		}
		issueID = pr.IssueID
	}
	total, err := issues_model.CountReviews(ctx, issues_model.FindReviewOptions{IssueID: issueID})
	if err != nil {
		return nil, ErrSnapshotUnavailable
	}
	rows, err := issues_model.FindReviews(ctx, issues_model.FindReviewOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: snapshotFetchCap},
		IssueID:     issueID,
	})
	if err != nil {
		return nil, ErrSnapshotUnavailable
	}
	after := l.cursorAfterID()
	var kept []*issues_model.Review
	remaining := 0
	for _, row := range rows {
		if row.ID <= after {
			continue
		}
		remaining++
		if len(kept) < l.limit {
			kept = append(kept, row)
		}
	}
	capped := len(rows) >= snapshotFetchCap && total > int64(len(rows))
	page := &sdk.SnapshotReviewPage{IssueID: snapshotIDString(issueID), Total: remaining}
	for _, row := range kept {
		converted, err := issues_model.NewReviewSnapshot(row, true, "", true)
		if err != nil {
			return nil, ErrSnapshotUnavailable
		}
		page.Items = append(page.Items, sdk.SnapshotReview{
			ID:            snapshotIDString(converted.ReviewID),
			IssueID:       snapshotIDString(converted.IssueID),
			Type:          converted.Type,
			ReviewerID:    snapshotIDString(converted.ReviewerID),
			CommitID:      converted.CommitID,
			Official:      converted.Official,
			Stale:         converted.Stale,
			Dismissed:     converted.Dismissed,
			ContentDigest: converted.ContentDigest,
			CreatedUnix:   converted.CreatedUnix,
			UpdatedUnix:   converted.UpdatedUnix,
			Visible:       true,
			Complete:      true,
		})
	}
	if !capped {
		page.Complete = len(kept) == remaining
	}
	return page, nil
}

func (l *snapshotLoader) checks(ctx context.Context, sha string) (*sdk.SnapshotCheckSet, error) {
	if !l.permission.CanRead(unit.TypeCode) {
		return nil, ErrSnapshotNotFound
	}
	statuses, count, err := git_model.GetLatestCommitStatus(ctx, l.repository.ID, sha, db.ListOptions{Page: 1, PageSize: snapshotFetchCap})
	if err != nil {
		return nil, ErrSnapshotUnavailable
	}
	after := l.cursorAfterID()
	var kept []*git_model.CommitStatus
	remaining := 0
	for _, status := range statuses {
		if status.ID <= after {
			continue
		}
		remaining++
		if len(kept) < l.limit {
			kept = append(kept, status)
		}
	}
	capped := len(statuses) >= snapshotFetchCap && count > int64(len(statuses))
	set := &sdk.SnapshotCheckSet{SHA: sha, Total: remaining}
	for _, status := range kept {
		converted, err := git_model.NewCheckSnapshot(status, true, "", true)
		if err != nil {
			return nil, ErrSnapshotUnavailable
		}
		set.Items = append(set.Items, sdk.SnapshotCheck{
			ID:          snapshotIDString(converted.CheckID),
			Index:       converted.Index,
			SHA:         converted.SHA,
			Context:     converted.Context,
			State:       converted.State,
			CreatorID:   snapshotIDString(converted.CreatorID),
			CreatedUnix: converted.CreatedUnix,
			UpdatedUnix: converted.UpdatedUnix,
			Visible:     true,
			Complete:    true,
		})
	}
	if !capped {
		set.Complete = len(kept) == remaining
	}
	return set, nil
}

func (l *snapshotLoader) refs(ctx context.Context, refs []string) ([]sdk.SnapshotRef, error) {
	codeReadable := l.permission.CanRead(unit.TypeCode)
	if _, err := os.Stat(l.repository.RepoPath()); err != nil {
		return nil, ErrSnapshotUnavailable
	}
	out := make([]sdk.SnapshotRef, 0, len(refs))
	for _, ref := range refs {
		if !codeReadable {
			converted, err := git_model.NewRefSnapshot(l.repository.ID, ref, "", false, false, git_model.CheckHiddenNoAccess, true)
			if err != nil {
				return nil, ErrSnapshotUnavailable
			}
			out = append(out, sdk.SnapshotRef{
				Ref:          converted.Ref,
				Visible:      false,
				HiddenReason: converted.HiddenReason,
				Complete:     true,
			})
			continue
		}
		oid, err := l.service.readRef(ctx, l.repository.RepoPath(), ref)
		if err != nil && !git.IsErrNotExist(err) {
			return nil, ErrSnapshotUnavailable
		}
		exists := err == nil
		converted, err := git_model.NewRefSnapshot(l.repository.ID, ref, oid, exists, true, "", true)
		if err != nil {
			return nil, ErrSnapshotUnavailable
		}
		out = append(out, sdk.SnapshotRef{
			Ref:      converted.Ref,
			OID:      converted.OID,
			Exists:   converted.Exists,
			Visible:  true,
			Complete: true,
		})
	}
	return out, nil
}
