#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Local build only; publication is owned by the GitHub workflow.
set -euo pipefail
if [[ $# != 3 ]]; then
  echo "Usage: $0 VERSION FULL_COMMIT NEW_OUTPUT_DIRECTORY" >&2
  exit 2
fi
version=$1
commit=$2
out=$3
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+-fountain\.[1-9][0-9]*$ ]]
[[ $commit =~ ^[0-9a-f]{40}$ ]]
[[ $out == /home/* && $(uname -sm) == 'Linux x86_64' ]]
[[ $(git rev-parse HEAD) == "$commit" ]]
[[ -z $(git status --porcelain --untracked-files=normal) ]]
for tool in go npm gcc make tar gzip jq sha256sum "${CONTAINER_ENGINE:-docker}"; do command -v "$tool" >/dev/null; done
mkdir "$out" # Refuse to overwrite an earlier attempt.
out=$(realpath "$out")
mkdir "$out/source"
# Extension fixtures use Unix sockets; keep temporary paths short even when
# the chosen output directory is deeply nested.
export TMPDIR="${TMPDIR:-$(mktemp -d "$HOME/.fountain-tmp.XXXXXX")}"
export npm_config_cache="${npm_config_cache:-$HOME/.npm}"
export GOTOOLCHAIN=go1.26.9
export GOCACHE="${GOCACHE:-$HOME/.cache/go-build}"
export GOPATH="${GOPATH:-$HOME/go}"
for path in "$TMPDIR" "$npm_config_cache" "$GOCACHE" "$GOPATH"; do [[ $path == /home/* ]]; done
source_name="fountain-$version-source.tar.gz"
git archive --format=tar --prefix="fountain-$version/" "$commit" | gzip -1n > "$out/$source_name"
[[ $(gzip -dc "$out/$source_name" | { git get-tar-commit-id; cat >/dev/null; }) == "$commit" ]]
tar -xzf "$out/$source_name" --strip-components=1 -C "$out/source"
cd "$out/source"
# Upstream format/tidy gates compare generated changes with Git. Give them an
# isolated baseline instead of accidentally inspecting the caller's checkout.
git init -q
git add --force .
git -c user.name=release-check -c user.email=release-check@example.invalid commit -qm 'Exact source baseline'
npm ci --no-audit --no-fund
# Reuse the upstream source gates, then the SDK/host fixtures relevant to this track.
make checks lint-frontend test-frontend
git diff --exit-code # Refuse any source mutation left by the gates.
go test -tags 'sqlite sqlite_unlock_notify' ./modules/extensions ./modules/setting ./services/extensions ./cmd ./contrib/extensions/example
(cd sdk && go test ./...)
mkdir "$out/example"
cp -R contrib/extensions/example/extension.json contrib/extensions/example/assets "$out/example/"
CGO_ENABLED=0 go build -trimpath -o "$out/example/backend" ./contrib/extensions/example
chmod 755 "$out/example/backend"
# Upstream Makefile archive identity; no .git or workstation state enters the build.
echo "$version+gitea-1.22.0" > VERSION
rm -rf "$out/source/.git" # Only the task-owned source-check baseline.
engine=${CONTAINER_ENGINE:-docker}
build=("$engine" build)
if [[ $engine == docker && ${GITHUB_ACTIONS:-} == true ]]; then
  build=(docker buildx build --load --cache-from type=gha,scope=fountain-runtime --cache-to type=gha,mode=max,scope=fountain-runtime)
fi
"${build[@]}" --platform linux/amd64 \
  --build-arg "RELEASE_VERSION=$version" --build-arg "SOURCE_REVISION=$commit" \
  --build-arg "SOURCE_URL=https://github.com/${GITHUB_REPOSITORY:-veighnsche/forgejo-ext}" \
  --iidfile "$out/image-id" -t "fountain-release:$version" .
image=$(cat "$out/image-id")
bash tools/fountain-image-test.sh "$image" "$version" "$commit" "$out"
# Copy the actual shipped executable, not a second compilation.
container=$("$engine" create "$image")
trap '"$engine" rm -fv "$container" >/dev/null' EXIT
"$engine" cp "$container:/app/gitea/gitea" "$out/fountain-$version-linux-amd64"
"$engine" cp "$container:/usr/share/licenses/fountain/LICENSE" "$out/LICENSE"
"$engine" rm -v "$container" >/dev/null
trap - EXIT
# Recover the shipped frontend, then reuse upstream's source distribution
# recipe (generated bindata and vendored Go dependencies, without recompiling).
"$out/fountain-$version-linux-amd64" --work-path "$out/source" \
  --custom-path "$out/source/custom" --config "$out/source/custom/conf/app.ini" \
  embedded extract --overwrite --include-vendored --destination "$out/source" 'public/**'
GZIP=-1 make SHASUM=sha256sum VERSION="$version+gitea-1.22.0" TAGS=bindata FORGEJO_GENERATE_SKIP_HASH=true sources-tarbal
prepared="forgejo-src-$version+gitea-1.22.0.tar.gz"
cp "dist/release/$prepared" "$out/$prepared"
gzip -1n "$out/fountain-$version-linux-amd64"
sha256sum "$out/$source_name" "$out/$prepared" "$out/fountain-$version-linux-amd64.gz" "$out/LICENSE" | sed "s|$out/||" > "$out/SHA256SUMS"
(cd "$out" && sha256sum -c SHA256SUMS)
printf 'Validated image: %s\nArtifacts: %s\n' "$image" "$out"
