// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package nativeoperation

import (
	"context"
	"encoding/json"
	"strconv"

	sdk "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	"forgejo.org/models/extensionauth"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	webhook_model "forgejo.org/models/webhook"
	api "forgejo.org/modules/structs"
	webhook_module "forgejo.org/modules/webhook"

	"code.forgejo.org/xorm/xorm"
	"github.com/google/uuid"
)

// ReadCreations scans retained native issue-event tasks, including failed HTTP
// deliveries. It returns only issue-scoped creation facts, never webhook
// configuration, raw payloads, request headers or response contents. A retained
// end is not a claim that deleted or never-recorded creation history exists.
func (s *Service) ReadCreations(ctx context.Context, decision extensionauth.SnapshotReadDecision, req sdk.CreationRecoveryRequest) (sdk.CreationRecoveryPage, error) {
	if sdk.ValidateCreationRecoveryRequest(req) != nil {
		return sdk.CreationRecoveryPage{}, ErrSnapshotInvalid
	}
	repositoryID, _ := strconv.ParseInt(req.RepositoryID, 10, 64)
	actorID, _ := strconv.ParseInt(req.ActorID, 10, 64)
	if actorID != decision.ActorID || repositoryID != decision.RepositoryID || !decision.CanReadFamilies([]string{sdk.SnapshotFamilyIssue}) {
		return sdk.CreationRecoveryPage{}, ErrSnapshotInvalid
	}
	repository, err := repo_model.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return sdk.CreationRecoveryPage{}, ErrSnapshotNotFound
		}
		return sdk.CreationRecoveryPage{}, ErrSnapshotUnavailable
	}
	if !decision.AllowsRepository(repository.ID, repository.IsPrivate) {
		return sdk.CreationRecoveryPage{}, ErrSnapshotNotFound
	}
	actor, err := user_model.GetUserByID(ctx, actorID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return sdk.CreationRecoveryPage{}, ErrSnapshotNotFound
		}
		return sdk.CreationRecoveryPage{}, ErrSnapshotUnavailable
	}
	if !actor.IsActive || actor.ProhibitLogin {
		return sdk.CreationRecoveryPage{}, ErrSnapshotNotFound
	}
	permission, err := access_model.GetUserRepoPermission(ctx, repository, actor)
	if err != nil {
		return sdk.CreationRecoveryPage{}, ErrSnapshotUnavailable
	}
	if !permission.CanRead(unit.TypeIssues) {
		return sdk.CreationRecoveryPage{}, ErrSnapshotNotFound
	}
	query := func() *xorm.Session {
		return db.GetEngine(ctx).Table(new(webhook_model.HookTask)).
			Join("INNER", "webhook", "webhook.id=hook_task.hook_id").
			Where("webhook.repo_id=? AND hook_task.event_type=?", repositoryID, webhook_module.HookEventIssues)
	}
	var oldest webhook_model.HookTask
	has, err := query().Select("hook_task.id").Asc("hook_task.id").Get(&oldest)
	if err != nil {
		return sdk.CreationRecoveryPage{}, ErrSnapshotUnavailable
	}
	page := sdk.CreationRecoveryPage{
		RepositoryID: req.RepositoryID, Creations: []sdk.NativeCreationObservation{},
		Complete: true, ProvenanceComplete: true, RetentionLimited: true,
	}
	if has {
		page.OldestRetainedTaskID = snapshotIDString(oldest.ID)
	}
	afterID, _ := strconv.ParseInt(req.AfterID, 10, 64)
	var tasks []*webhook_model.HookTask
	err = query().Select("hook_task.id,hook_task.uuid,hook_task.payload_version,hook_task.payload_content").
		And("hook_task.id>?", afterID).Asc("hook_task.id").Limit(req.Limit + 1).Find(&tasks)
	if err != nil {
		return sdk.CreationRecoveryPage{}, ErrSnapshotUnavailable
	}
	if len(tasks) > req.Limit {
		page.Complete = false
		tasks = tasks[:req.Limit]
	}
	for _, task := range tasks {
		page.NextCursor = snapshotIDString(task.ID)
		if task.PayloadVersion != 2 {
			page.ProvenanceComplete = false
			continue
		}
		var payload api.IssuePayload
		if json.Unmarshal([]byte(task.PayloadContent), &payload) != nil || payload.Action == "" {
			page.ProvenanceComplete = false
			continue
		}
		switch payload.Action {
		case api.HookIssueOpened:
		case api.HookIssueClosed, api.HookIssueReOpened, api.HookIssueEdited, api.HookIssueAssigned, api.HookIssueUnassigned,
			api.HookIssueLabelUpdated, api.HookIssueLabelCleared, api.HookIssueSynchronized, api.HookIssueMilestoned,
			api.HookIssueDemilestoned, api.HookIssueReviewed, api.HookIssueReviewRequested, api.HookIssueReviewRequestRemoved:
			continue
		default:
			page.ProvenanceComplete = false
			continue
		}
		observation, complete, err := nativeCreation(ctx, repositoryID, task, &payload)
		if err != nil {
			return sdk.CreationRecoveryPage{}, err
		}
		if !complete {
			page.ProvenanceComplete = false
		}
		if observation != nil {
			page.Creations = append(page.Creations, *observation)
		}
	}
	return page, nil
}

// nativeCreation binds the original native event to the current immutable
// issue identity and creation-body provenance. Current title/body
// may differ: the original digests are preserved for the consumer to compare.
func nativeCreation(ctx context.Context, repositoryID int64, task *webhook_model.HookTask, payload *api.IssuePayload) (*sdk.NativeCreationObservation, bool, error) {
	if parsed, err := uuid.Parse(task.UUID); err != nil || parsed.String() != task.UUID {
		return nil, false, nil
	}
	if payload.Repository == nil || payload.Repository.ID != repositoryID || payload.Repository.Permissions == nil ||
		payload.Issue == nil || payload.Issue.ID <= 0 || payload.Issue.Index <= 0 || payload.Issue.Index != payload.Index ||
		payload.Issue.Poster == nil || payload.Sender == nil || payload.Sender.ID <= 0 || payload.Sender.ID != payload.Issue.Poster.ID ||
		payload.Issue.PullRequest != nil || payload.Issue.OriginalAuthorID != 0 || payload.Issue.OriginalAuthor != "" || payload.Issue.Created.Unix() <= 0 {
		return nil, false, nil
	}
	observation := &sdk.NativeCreationObservation{
		TaskID: snapshotIDString(task.ID), DeliveryID: task.UUID, RepositoryID: snapshotIDString(repositoryID),
		IssueID: snapshotIDString(payload.Issue.ID), IssueIndex: snapshotIDString(payload.Issue.Index),
		CreatorID: snapshotIDString(payload.Sender.ID), CreatorWrite: payload.Repository.Permissions.Push,
		CreatedUnix: payload.Issue.Created.Unix(), TitleDigest: issues_model.ContentDigest(payload.Issue.Title), ContentDigest: issues_model.ContentDigest(payload.Issue.Body),
	}
	issue, err := issues_model.GetIssueByID(ctx, payload.Issue.ID)
	if err != nil {
		if issues_model.IsErrIssueNotExist(err) {
			return nil, false, nil
		}
		return nil, false, ErrSnapshotUnavailable
	}
	if issue.RepoID != repositoryID || issue.Index != payload.Issue.Index || issue.IsPull ||
		issue.PosterID != payload.Sender.ID || issue.OriginalAuthorID != 0 || issue.OriginalAuthor != "" || int64(issue.CreatedUnix) != observation.CreatedUnix {
		return nil, false, nil
	}
	var history []*issues_model.ContentHistory
	err = db.GetEngine(ctx).Where("issue_id=? AND comment_id=0 AND is_first_created=?", issue.ID, true).Limit(2).Find(&history)
	if err != nil {
		return nil, false, ErrSnapshotUnavailable
	}
	if len(history) == 0 {
		// Normal creation writes no body history. Its unchanged initial body
		// corroborates the original native task; an edit/revert cannot do so.
		observation.Verified = issue.ContentVersion == 0 && issues_model.ContentDigest(issue.Content) == observation.ContentDigest
	} else if len(history) == 1 {
		row := history[0]
		observation.Verified = !row.IsDeleted && row.PosterID == payload.Sender.ID && int64(row.EditedUnix) == observation.CreatedUnix &&
			issues_model.ContentDigest(row.ContentText) == observation.ContentDigest
	}
	return observation, observation.Verified, nil
}
