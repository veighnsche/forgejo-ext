// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package db

type ErrRepoCondAndRepoID struct{}

func (err ErrRepoCondAndRepoID) Error() string {
	return "Both RepoCond and RepoID have been set, these options are mutually exclusive"
}
