#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
set -euo pipefail
# Check the worktree and index separately: staged generator output can differ
# even when the working file has been put back to its original HEAD content.
git diff --exit-code HEAD --
git diff --cached --exit-code HEAD --
if [[ -n $(git ls-files --others --exclude-standard) ]]; then
  git ls-files --others --exclude-standard >&2
  echo 'Unexpected untracked source in qualifying checkout' >&2
  exit 1
fi
