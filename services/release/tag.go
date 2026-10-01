// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package release

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/graceful"
	"forgejo.org/modules/log"
	"forgejo.org/modules/queue"
	repo_module "forgejo.org/modules/repository"
	operation_service "forgejo.org/services/nativeoperation"

	"xorm.io/builder"
)

type TagSyncOptions struct {
	RepoID int64
}

// tagSyncQueue represents a queue to handle tag sync jobs.
var tagSyncQueue *queue.WorkerPoolQueue[*TagSyncOptions]

func handlerTagSync(items ...*TagSyncOptions) []*TagSyncOptions {
	var unhandled []*TagSyncOptions
	for _, opts := range items {
		// Each tag sync owns the reservation before its database effects;
		// busy items stay queued instead of being dropped.
		err := operation_service.Default().WithOrdinaryOwnership(
			graceful.GetManager().ShutdownContext(),
			operation_service.FamilyRefSync,
			fmt.Sprintf("%d/tags", opts.RepoID),
			operation_service.Scope{
				Family:       operation_service.FamilyRefSync,
				RepositoryID: opts.RepoID,
			},
			func(ctx context.Context) error {
				return repo_module.SyncRepoTags(ctx, opts.RepoID)
			})
		if err != nil {
			if operation_service.IsBusy(err) {
				unhandled = append(unhandled, opts)
				continue
			}
			log.Error("syncRepoTags [%d] failed: %v", opts.RepoID, err)
		}
	}
	return unhandled
}

func addRepoToTagSyncQueue(repoID int64) error {
	return tagSyncQueue.Push(&TagSyncOptions{
		RepoID: repoID,
	})
}

func initTagSyncQueue(ctx context.Context) error {
	tagSyncQueue = queue.CreateUniqueQueue(ctx, "tag_sync", handlerTagSync)
	if tagSyncQueue == nil {
		return errors.New("unable to create tag_sync queue")
	}
	go graceful.GetManager().RunWithCancel(tagSyncQueue)

	return nil
}

func AddAllRepoTagsToSyncQueue(ctx context.Context) error {
	if err := db.Iterate(ctx, builder.Eq{"is_empty": false}, func(ctx context.Context, repo *repo_model.Repository) error {
		return addRepoToTagSyncQueue(repo.ID)
	}); err != nil {
		return fmt.Errorf("run sync all tags failed: %v", err)
	}
	return nil
}
