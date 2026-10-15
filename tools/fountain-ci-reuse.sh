#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# A maintained-branch push reuses only its exact commit's successful push CI.
set -euo pipefail
attempt=${GITHUB_RUN_ATTEMPT:-1}
[[ $attempt =~ ^[1-9][0-9]*$ ]]
started=$(gh api "repos/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID/attempts/$attempt" --jq .run_started_at)
printf 'started_at=%s\n' "$(date --date="$started" +%s)" >> "$GITHUB_OUTPUT"
reused=false
if [[ ${GITHUB_EVENT_NAME:-} == push && ${GITHUB_REF:-} == refs/heads/codex/fountain-extensions && ${GITHUB_RUN_ATTEMPT:-} == 1 && ${GITHUB_SHA:-} =~ ^[0-9a-f]{40}$ ]]; then
  tag="ci/fountain-$GITHUB_SHA"
  result=$(mktemp "${TMPDIR:-$HOME}/fountain-ci-reuse.XXXXXXXX")
  trap 'rm -f "$result"' EXIT
  if gh api "repos/$GITHUB_REPOSITORY/actions/workflows/fountain-ci.yml/runs?head_sha=$GITHUB_SHA&branch=$tag&event=push&per_page=1" > "$result"; then
    if jq -e --arg sha "$GITHUB_SHA" --arg tag "$tag" '
      .workflow_runs | length == 1 and
      (.[0] | (.id | type == "number" and . > 0) and .head_sha == $sha and .head_branch == $tag and
        .event == "push" and .path == ".github/workflows/fountain-ci.yml" and
        .status == "completed" and .conclusion == "success")
    ' "$result" >/dev/null; then
      reused=true
      printf 'Reusing push CI run %s for exact commit %s\n' "$(jq -r '.workflow_runs[0].id' "$result")" "$GITHUB_SHA"
    fi
  fi
fi
printf 'reused=%s\n' "$reused" >> "$GITHUB_OUTPUT"
