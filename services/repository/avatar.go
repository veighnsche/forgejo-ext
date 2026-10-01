// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/avatar"
	"forgejo.org/modules/log"
	"forgejo.org/modules/storage"
	operation_service "forgejo.org/services/nativeoperation"
)

// UploadAvatar saves custom avatar for repository.
// FIXME: split uploads to different subdirs in case we have massive number of repos.
func UploadAvatar(ctx context.Context, repo *repo_model.Repository, data []byte) error {
	avatarData, err := avatar.ProcessAvatarImage(data)
	if err != nil {
		return err
	}

	newAvatar := avatar.HashAvatar(repo.ID, data)
	if repo.Avatar == newAvatar { // upload the same picture
		return nil
	}

	// One settings update owns the reservation before the avatar
	// transaction and storage writes, advancing the revision so stale
	// observations go stale.
	return operation_service.Default().WithOrdinaryOwnership(ctx,
		operation_service.FamilyRepoSettings,
		fmt.Sprintf("%d/avatar", repo.ID),
		operation_service.Scope{
			Family:       operation_service.FamilyRepoSettings,
			RepositoryID: repo.ID,
		},
		func(ctx context.Context) error {
			return uploadAvatarOwned(ctx, repo, avatarData, newAvatar)
		})
}

// uploadAvatarOwned records the avatar hash and stores the image under
// the caller's ownership.
func uploadAvatarOwned(ctx context.Context, repo *repo_model.Repository, avatarData []byte, newAvatar string) error {
	ctx, committer, err := db.TxContext(ctx)
	if err != nil {
		return err
	}
	defer committer.Close()

	oldAvatarPath := repo.CustomAvatarRelativePath()

	// Users can upload the same image to other repo - prefix it with ID
	// Then repo will be removed - only it avatar file will be removed
	repo.Avatar = newAvatar
	if err := repo_model.UpdateRepositoryCols(ctx, repo, "avatar"); err != nil {
		return fmt.Errorf("UploadAvatar: Update repository avatar: %w", err)
	}

	if err := storage.SaveFrom(storage.RepoAvatars, repo.CustomAvatarRelativePath(), func(w io.Writer) error {
		_, err := w.Write(avatarData)
		return err
	}); err != nil {
		return fmt.Errorf("UploadAvatar %s failed: Failed to remove old repo avatar %s: %w", repo.RepoPath(), newAvatar, err)
	}

	if len(oldAvatarPath) > 0 {
		if err := storage.RepoAvatars.Delete(oldAvatarPath); err != nil {
			return fmt.Errorf("UploadAvatar: Failed to remove old repo avatar %s: %w", oldAvatarPath, err)
		}
	}

	return committer.Commit()
}

// DeleteAvatar deletes the repos's custom avatar.
func DeleteAvatar(ctx context.Context, repo *repo_model.Repository) error {
	// Avatar not exists
	if len(repo.Avatar) == 0 {
		return nil
	}

	avatarPath := repo.CustomAvatarRelativePath()
	log.Trace("DeleteAvatar[%d]: %s", repo.ID, avatarPath)

	// One settings update owns the reservation before the avatar
	// transaction and storage delete, advancing the revision so stale
	// observations go stale.
	return operation_service.Default().WithOrdinaryOwnership(ctx,
		operation_service.FamilyRepoSettings,
		fmt.Sprintf("%d/avatar-delete", repo.ID),
		operation_service.Scope{
			Family:       operation_service.FamilyRepoSettings,
			RepositoryID: repo.ID,
		},
		func(ctx context.Context) error {
			return deleteAvatarOwned(ctx, repo, avatarPath)
		})
}

// deleteAvatarOwned clears the avatar hash and deletes the image under
// the caller's ownership.
func deleteAvatarOwned(ctx context.Context, repo *repo_model.Repository, avatarPath string) error {
	ctx, committer, err := db.TxContext(ctx)
	if err != nil {
		return err
	}
	defer committer.Close()

	repo.Avatar = ""
	if err := repo_model.UpdateRepositoryCols(ctx, repo, "avatar"); err != nil {
		return fmt.Errorf("DeleteAvatar: Update repository avatar: %w", err)
	}

	if err := storage.RepoAvatars.Delete(avatarPath); err != nil {
		return fmt.Errorf("DeleteAvatar: Failed to remove %s: %w", avatarPath, err)
	}

	return committer.Commit()
}

// RemoveRandomAvatars removes the randomly generated avatars that were created for repositories
func RemoveRandomAvatars(ctx context.Context) error {
	return db.Iterate(ctx, nil, func(ctx context.Context, repository *repo_model.Repository) error {
		select {
		case <-ctx.Done():
			return db.ErrCancelledf("before random avatars removed for %s", repository.FullName())
		default:
		}
		stringifiedID := strconv.FormatInt(repository.ID, 10)
		if repository.Avatar == stringifiedID {
			return DeleteAvatar(ctx, repository)
		}
		return nil
	})
}

// generateAvatar generates the avatar from a template repository
func generateAvatar(ctx context.Context, templateRepo, generateRepo *repo_model.Repository) error {
	file, err := storage.RepoAvatars.Open(templateRepo.CustomAvatarRelativePath())
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	generateRepo.Avatar = avatar.HashAvatar(generateRepo.ID, data)
	if _, err := storage.Copy(storage.RepoAvatars, generateRepo.CustomAvatarRelativePath(), storage.RepoAvatars, templateRepo.CustomAvatarRelativePath()); err != nil {
		return err
	}

	return repo_model.UpdateRepositoryCols(ctx, generateRepo, "avatar")
}
