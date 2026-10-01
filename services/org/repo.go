// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package org

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/models"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	operation_service "forgejo.org/services/nativeoperation"
)

// TeamAddRepository adds new repository to team of organization.
func TeamAddRepository(ctx context.Context, t *organization.Team, repo *repo_model.Repository) (err error) {
	if repo.OwnerID != t.OrgID {
		return errors.New("repository does not belong to organization")
	} else if organization.HasTeamRepo(ctx, t.OrgID, t.ID, repo.ID) {
		return nil
	}

	// One authority writer owns the team permission change before its
	// effects, advancing the native revision so old permission
	// observations go stale.
	return operation_service.WithAuthorityOwnership(ctx, fmt.Sprintf("team/%d/repo/%d", t.ID, repo.ID), repo.ID, func(ctx context.Context) error {
		return db.WithTx(ctx, func(ctx context.Context) error {
			return models.AddRepository(ctx, t, repo)
		})
	})
}
