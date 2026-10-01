// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"fmt"
	"slices"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	actions_service "forgejo.org/services/actions"
	operation_service "forgejo.org/services/nativeoperation"
)

// UpdateRepositoryUnits updates a repository's units
func UpdateRepositoryUnits(ctx context.Context, repo *repo_model.Repository, units []repo_model.RepoUnit, deleteUnitTypes []unit.Type) (err error) {
	// One settings update owns the reservation before its database effects,
	// advancing the revision so stale observations go stale. Nested Actions
	// schedule updates reuse this ownership.
	return operation_service.Default().WithOrdinaryOwnership(ctx,
		operation_service.FamilyRepoSettings,
		fmt.Sprintf("%d/units", repo.ID),
		operation_service.Scope{
			Family:       operation_service.FamilyRepoSettings,
			RepositoryID: repo.ID,
		},
		func(ctx context.Context) error {
			return updateRepositoryUnitsOwned(ctx, repo, units, deleteUnitTypes)
		})
}

func updateRepositoryUnitsOwned(ctx context.Context, repo *repo_model.Repository, units []repo_model.RepoUnit, deleteUnitTypes []unit.Type) (err error) {
	ctx, committer, err := db.TxContext(ctx)
	if err != nil {
		return err
	}
	defer committer.Close()

	// Delete existing settings of units before adding again
	for _, u := range units {
		deleteUnitTypes = append(deleteUnitTypes, u.Type)
	}

	if slices.Contains(deleteUnitTypes, unit.TypeActions) {
		if err := actions_service.CleanRepoScheduleTasks(ctx, repo, true); err != nil {
			log.Error("CleanRepoScheduleTasks: %v", err)
		}
	}

	for _, u := range units {
		if u.Type == unit.TypeActions {
			if err := actions_service.DetectAndHandleSchedules(ctx, repo); err != nil {
				log.Error("DetectAndHandleSchedules: %v", err)
			}
			break
		}
	}

	if _, err = db.GetEngine(ctx).Where("repo_id = ?", repo.ID).In("type", deleteUnitTypes).Delete(new(repo_model.RepoUnit)); err != nil {
		return err
	}

	if len(units) > 0 {
		if err = db.Insert(ctx, units); err != nil {
			return err
		}
	}

	return committer.Commit()
}
