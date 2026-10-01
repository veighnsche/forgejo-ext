// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	execcontext "forgejo.org/modules/nativeoperation"
)

// FamilyCollaboration is the ordinary writer family for native
// collaboration inputs: issues, comments, dependencies, PR metadata,
// reviews and conversations, plus the labels, milestones, assignees,
// locks, pins, project assignments and content-history records that
// version them or gate eligibility. Every mapped collaboration writer
// claims this family before its effects, advancing the native revision
// so an old accepted-input observation cannot authorize a competing
// conditional write. Native text and relations stay canonical; this
// family adds no approval fields and interprets no factory decisions.
const FamilyCollaboration = "collaboration"

// CrashPointCollaborationAfterEffects pauses one top-level collaboration
// writer after its effects commit and before its owner releases. It is a
// disclosed test instrument for the interrupted collaboration-writer
// recovery proof; production never sets the barrier variables.
const CrashPointCollaborationAfterEffects = "collaboration-after-effects"

// Collaboration resource labels. Each label names the operation's anchor
// entities with positive integer IDs; the Scope carries the repository.
// Labels with unknowable IDs (creates), multi-entity batches and
// cross-family remaps still order before their effects, but offline
// recovery leaves them fenced.
const (
	// CollabOpCreate marks a create whose row ID is unknowable pre-insert.
	CollabOpCreate = "create"
)

// IssueResource names one issue-row writer: title, content, ref, status,
// deadline, delete, assignee, milestone, labels, pin, lock, project or
// attachment association on the named issue.
func IssueResource(issueID int64, op string) string {
	return fmt.Sprintf("issue/%d/%s", issueID, op)
}

// IssueCreateResource names one issue creation in the named repository.
// The issue ID is unknowable pre-insert, so recovery fences it.
func IssueCreateResource(repoID int64) string {
	return fmt.Sprintf("issue/new/%d", repoID)
}

// CommentResource names one comment writer: update, delete, conversation
// or attachment on the named comment.
func CommentResource(commentID int64, op string) string {
	return fmt.Sprintf("comment/%d/%s", commentID, op)
}

// CommentCreateResource names one comment creation on the named issue.
// The comment ID is unknowable pre-insert, so recovery fences it.
func CommentCreateResource(issueID int64) string {
	return fmt.Sprintf("comment/new/%d", issueID)
}

// DependencyResource names one dependency edge writer between the two
// named issues. The edge may be present or absent; both are valid
// single-transaction end states.
func DependencyResource(issueID, depID int64) string {
	return fmt.Sprintf("dependency/%d/%d", issueID, depID)
}

// PullResource names one PR-metadata writer: retarget, close, update,
// refresh, automerge, manual or edits on the named pull request.
func PullResource(prID int64, op string) string {
	return fmt.Sprintf("pull/%d/%s", prID, op)
}

// PullCreateResource names one PR creation on the named issue. The PR ID
// is unknowable pre-insert, so recovery fences it.
func PullCreateResource(issueID int64) string {
	return fmt.Sprintf("pull/new/%d", issueID)
}

// ReviewResource names one review writer: dismiss on the named review.
func ReviewResource(reviewID int64, op string) string {
	return fmt.Sprintf("review/%d/%s", reviewID, op)
}

// ReviewRequestResource names one user review-request add or remove on
// the named issue for the named reviewer. Requests address reviewers by
// identity, not by review row, so the label carries both IDs.
func ReviewRequestResource(issueID, reviewerID int64) string {
	return fmt.Sprintf("review/%d/request-user/%d", issueID, reviewerID)
}

// ReviewTeamRequestResource names one team review-request add or remove
// on the named issue for the named team.
func ReviewTeamRequestResource(issueID, teamID int64) string {
	return fmt.Sprintf("review/%d/request-team/%d", issueID, teamID)
}

// ReviewSubmitResource names one review submission on the named issue.
// The review ID is unknowable pre-insert, so recovery fences it.
func ReviewSubmitResource(issueID int64) string {
	return fmt.Sprintf("review/new/%d", issueID)
}

// ReviewDeleteResource names one single-transaction review deletion on
// the named issue. A present or absent review row over the intact issue
// are both valid atomic end states.
func ReviewDeleteResource(issueID, reviewID int64) string {
	return fmt.Sprintf("review/%d/%d/delete", issueID, reviewID)
}

// ReactionResource names one reaction writer on the named issue, with
// commentID 0 for an issue-level reaction.
func ReactionResource(issueID, commentID, doerID int64) string {
	return fmt.Sprintf("reaction/%d/%d/%d", issueID, commentID, doerID)
}

// LabelResource names one label-row writer: update on the named label,
// or create on the named repository.
func LabelResource(id int64, op string) string {
	return fmt.Sprintf("label/%d/%s", id, op)
}

// MilestoneResource names one milestone writer: update or status on the
// named milestone, or create/delete on the named repository.
func MilestoneResource(id int64, op string) string {
	return fmt.Sprintf("milestone/%d/%s", id, op)
}

// Attachment operations carried in collaboration resource labels. Issue
// and comment attachments are part of native accepted-input records;
// release assets ride the same attachment row writer, so standalone
// asset operations claim here too (assets attached inside a release
// flow nest under that flow's owner instead).
const (
	// CollabAttachmentCreate uploads one attachment row. The storage
	// UUID is invented inside the write, so offline recovery fences it.
	CollabAttachmentCreate = "create"
	// CollabAttachmentDelete removes one attachment row by ID. The row
	// is attributable both ways.
	CollabAttachmentDelete = "delete"
	// CollabAttachmentUpdate edits one attachment row by ID. The applied
	// values are unknowable, so offline recovery fences it.
	CollabAttachmentUpdate = "update"
)

// AttachmentCreateResource names one attachment upload on the named
// repository. The Scope carries the repository.
func AttachmentCreateResource(repoID int64) string {
	return fmt.Sprintf("attachment/new/%d", repoID)
}

// AttachmentResource names one attachment row update by ID. The Scope
// carries the repository.
func AttachmentResource(attachID int64, op string) string {
	return fmt.Sprintf("attachment/%d/%s", attachID, op)
}

// HistoryResource names one content-history soft-delete on the named
// history row.
func HistoryResource(historyID int64) string {
	return fmt.Sprintf("history/%d/delete", historyID)
}

// CollabBatchResource names one multi-entity collaboration batch:
// orphan-fix, external-remap, push-commit or pr-sweep. Batches order
// before their effects, but recovery fences them: no single-entity
// reconciliation.
func CollabBatchResource(op string) string {
	return "batch/" + op
}

// collabClaim is one parsed collaboration resource label.
type collabClaim struct {
	kind string // issue, comment, dependency, pull, review, reaction, label, milestone, history, attachment or batch
	id   int64  // anchor entity ID (issue, comment, PR, review, label, milestone or history row)
	id2  int64  // second entity ID (dependency target, review delete pair)
	op   string // operation within the kind
}

// parseCollabResource strictly parses one collaboration resource label.
// Any deviation fences: recovery must never guess which update an owner
// ran. The service mirrors in services/issue and services/pull build the
// same shapes without importing this package; their wire tests pin them.
func parseCollabResource(resource string) (collabClaim, error) {
	var claim collabClaim
	parts := strings.Split(resource, "/")
	if len(parts) < 2 {
		return claim, fmt.Errorf("unparseable collaboration resource %q", resource)
	}
	positive := func(s string) (int64, bool) {
		id, err := strconv.ParseInt(s, 10, 64)
		return id, err == nil && id > 0
	}
	switch parts[0] {
	case "issue":
		if len(parts) == 3 && parts[1] == "new" {
			id, ok := positive(parts[2])
			if !ok {
				break
			}
			return collabClaim{kind: "issue", id: id, op: "create"}, nil
		}
		if len(parts) != 3 {
			break
		}
		id, ok := positive(parts[1])
		if !ok {
			break
		}
		switch parts[2] {
		case "title", "content", "ref", "status", "deadline",
			"delete", "assignee", "milestone", "labels", "pin", "lock",
			"project", "attachment":
			claim = collabClaim{kind: "issue", id: id, op: parts[2]}
			return claim, nil
		}
	case "comment":
		if len(parts) == 3 && parts[1] == "new" {
			id, ok := positive(parts[2])
			if !ok {
				break
			}
			return collabClaim{kind: "comment", id: id, op: "create"}, nil
		}
		if len(parts) != 3 {
			break
		}
		id, ok := positive(parts[1])
		if !ok {
			break
		}
		switch parts[2] {
		case "update", "delete", "conversation", "attachment":
			return collabClaim{kind: "comment", id: id, op: parts[2]}, nil
		}
	case "dependency":
		if len(parts) != 3 {
			break
		}
		id, ok := positive(parts[1])
		id2, ok2 := positive(parts[2])
		if !ok || !ok2 {
			break
		}
		return collabClaim{kind: "dependency", id: id, id2: id2}, nil
	case "pull":
		if len(parts) == 3 && parts[1] == "new" {
			id, ok := positive(parts[2])
			if !ok {
				break
			}
			return collabClaim{kind: "pull", id: id, op: "create"}, nil
		}
		if len(parts) != 3 {
			break
		}
		id, ok := positive(parts[1])
		if !ok {
			break
		}
		switch parts[2] {
		case "retarget", "close", "update", "refresh", "automerge", "manual", "edits":
			return collabClaim{kind: "pull", id: id, op: parts[2]}, nil
		}
	case "review":
		if len(parts) == 3 && parts[1] == "new" {
			id, ok := positive(parts[2])
			if !ok {
				break
			}
			return collabClaim{kind: "review", id: id, op: "submit"}, nil
		}
		if len(parts) == 4 && parts[3] == "delete" {
			id, ok := positive(parts[1])
			id2, ok2 := positive(parts[2])
			if !ok || !ok2 {
				break
			}
			return collabClaim{kind: "review", id: id, id2: id2, op: "delete"}, nil
		}
		if len(parts) == 4 && (parts[2] == "request-user" || parts[2] == "request-team") {
			id, ok := positive(parts[1])
			id2, ok2 := positive(parts[3])
			if !ok || !ok2 {
				break
			}
			return collabClaim{kind: "review", id: id, id2: id2, op: parts[2]}, nil
		}
		if len(parts) != 3 {
			break
		}
		id, ok := positive(parts[1])
		if !ok {
			break
		}
		if parts[2] == "dismiss" {
			return collabClaim{kind: "review", id: id, op: parts[2]}, nil
		}
	case "reaction":
		if len(parts) != 4 {
			break
		}
		id, ok := positive(parts[1])
		commentID, err := strconv.ParseInt(parts[2], 10, 64)
		_, ok2 := positive(parts[3])
		if !ok || err != nil || commentID < 0 || !ok2 {
			break
		}
		// The validated doer ID rides in op; recovery reconciles
		// reactions from the issue/comment anchors only.
		return collabClaim{kind: "reaction", id: id, id2: commentID, op: parts[3]}, nil
	case "label":
		if len(parts) != 3 {
			break
		}
		id, ok := positive(parts[1])
		if !ok {
			break
		}
		switch parts[2] {
		case "create", "update", "delete":
			return collabClaim{kind: "label", id: id, op: parts[2]}, nil
		}
	case "milestone":
		if len(parts) != 3 {
			break
		}
		id, ok := positive(parts[1])
		if !ok {
			break
		}
		switch parts[2] {
		case "create", "update", "status", "delete":
			return collabClaim{kind: "milestone", id: id, op: parts[2]}, nil
		}
	case "history":
		if len(parts) != 3 || parts[2] != "delete" {
			break
		}
		id, ok := positive(parts[1])
		if !ok {
			break
		}
		return collabClaim{kind: "history", id: id, op: "delete"}, nil
	case "attachment":
		if len(parts) == 3 && parts[1] == "new" {
			id, ok := positive(parts[2])
			if !ok {
				break
			}
			return collabClaim{kind: "attachment", id: id, op: CollabAttachmentCreate}, nil
		}
		if len(parts) != 3 {
			break
		}
		id, ok := positive(parts[1])
		if !ok {
			break
		}
		switch parts[2] {
		case CollabAttachmentDelete, CollabAttachmentUpdate:
			return collabClaim{kind: "attachment", id: id, op: parts[2]}, nil
		}
	case "batch":
		if len(parts) != 2 {
			break
		}
		switch parts[1] {
		case "orphan-fix", "external-remap", "push-commit", "pr-sweep":
			return collabClaim{kind: "batch", op: parts[1]}, nil
		}
	}
	return claim, fmt.Errorf("unparseable collaboration resource %q", resource)
}

// WithCollaborationOwnership claims the idle reservation for one ordinary
// collaboration writer and releases it after the writer returns. A busy
// reservation or offline inhibition refuses before any effect. Nested
// calls reuse the enclosing ownership without claiming again and without
// pausing at the crash barrier: only the top-level collaboration claim
// can hold the barrier point.
func WithCollaborationOwnership(ctx context.Context, resource string, repositoryID int64, fn func(ctx context.Context) error) error {
	if execcontext.FromContext(ctx) != nil {
		return fn(ctx)
	}
	scope := Scope{RepositoryID: repositoryID}
	return Default().WithOrdinaryOwnership(ctx, FamilyCollaboration, resource, scope, func(ctx context.Context) error {
		if err := fn(ctx); err != nil {
			return err
		}
		return TestCrashBarrier(CrashPointCollaborationAfterEffects)
	})
}
