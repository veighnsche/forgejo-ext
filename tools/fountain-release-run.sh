#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Drive a Fountain release from one command: preflight, tag, dispatch, watch.
# Usage: bash tools/fountain-release-run.sh VERSION [--dry-run] [--yes] [--move-tag]
# VERSION looks like 15.0.10-fountain.2 (the leading v is added for the tag).
# --dry-run validates only (source gates, build once, test image) and publishes
# nothing; it needs no tag. --yes skips the confirmation prompt.
# --move-tag repoints an existing tag at the current commit for a retry.
set -euo pipefail

version=${1:-}
dry_run=0
yes=0
move_tag=0
for arg in "${@:2}"; do
  case $arg in
    --dry-run) dry_run=1 ;;
    --yes) yes=1 ;;
    --move-tag) move_tag=1 ;;
    *) echo "Usage: $0 VERSION [--dry-run] [--yes] [--move-tag]" >&2; exit 2 ;;
  esac
done
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+-fountain\.[1-9][0-9]*$ ]] || {
  echo "release: VERSION '$version' must look like 15.0.10-fountain.2" >&2; exit 2;
}

cd "$(git rev-parse --show-toplevel)"
branch=codex/fountain-extensions
[[ $(git branch --show-current) == "$branch" ]] || {
  echo "release: run from the maintained branch '$branch'" >&2; exit 2;
}
bash tools/fountain-check-clean.sh
repository=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
commit=$(git rev-parse HEAD)
tag="v$version"

remote=$(git ls-remote origin "refs/heads/$branch" | cut -f1)
if [[ $remote != "$commit" ]]; then
  echo "release: pushing $commit to $branch first"
  git push origin "HEAD:refs/heads/$branch"
fi

if git rev-parse --verify "refs/tags/$tag" >/dev/null 2>&1; then
  pointed=$(git rev-parse "$tag^{commit}")
  if [[ $pointed != "$commit" ]]; then
    (( move_tag == 1 )) || {
      echo "release: tag $tag points at $pointed, not $commit (retry with --move-tag)" >&2; exit 2;
    }
  fi
fi
if (( dry_run == 0 )); then
  if (( move_tag == 1 )) || ! git rev-parse --verify "refs/tags/$tag" >/dev/null 2>&1; then
    git tag -f "$tag" "$commit"
    git push -f origin "refs/tags/$tag"
  fi
fi

if (( dry_run == 1 )); then mode=dry-run; else mode=publish; fi
if (( yes == 0 )); then
  [[ -t 0 ]] || { echo 'release: refusing to run without --yes on a non-terminal' >&2; exit 2; }
  printf 'Fountain %s (%s) from %s on %s? [yes/NO] ' "$version" "$mode" "$commit" "$branch"
  read -r answer
  [[ $answer == yes ]] || exit 2
fi

if (( dry_run == 1 )); then
  gh workflow run fountain-release.yml --ref "$branch" -f tag="$tag" -F dry_run=true --repo "$repository"
else
  gh workflow run fountain-release.yml --ref "$branch" -f tag="$tag" --repo "$repository"
fi
sleep 10
run=$(gh run list --workflow fountain-release.yml --branch "$branch" --limit 5 \
  --json databaseId,headSha,createdAt --jq "[.[] | select(.headSha == \"$commit\")][0].databaseId" \
  --repo "$repository")
[[ -n $run && $run != null ]] || { echo 'release: dispatched run not found; check the Actions tab' >&2; exit 1; }
url="https://github.com/$repository/actions/runs/$run"
printf 'Watching %s\n' "$url"
if timeout --signal=TERM --kill-after=30s 3900s gh run watch "$run" --repo "$repository"; then
  if (( dry_run == 1 )); then
    echo "Fountain $version validated from $commit; nothing was published."
  else
    gh release view "$tag" --repo "$repository" --json url --jq '"Published: " + .url'
    printf 'Release metadata: https://github.com/%s/releases/download/%s/release.json\n' "$repository" "$tag"
  fi
  exit 0
fi
result=$?
if (( result == 124 || result == 137 )); then
  printf 'Watch timed out; the run continues at %s\n' "$url" >&2
  exit 2
fi
printf 'Release run failed: %s\n' "$url" >&2
gh run view "$run" --repo "$repository" --log-failed 2>/dev/null | grep -a -A 120 'container log (tail)' | cut -c1-250 | head -n 130 || true
gh run view "$run" --repo "$repository" --log-failed 2>/dev/null | grep -a '##\[error\]' | cut -c1-250 | head -n 10 || true
exit 1
