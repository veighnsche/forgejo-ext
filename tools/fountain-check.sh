#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ $# != 1 || ${FOUNTAIN_CHECK_ISOLATED:-} != 1 ]]; then
  echo 'Usage: FOUNTAIN_CHECK_ISOLATED=1 bash tools/fountain-check.sh {source|unit|database|browser|packaging|release}' >&2
  echo 'Use a disposable committed checkout; these checks run source generators.' >&2
  exit 2
fi
case $1 in source|unit|database|browser|packaging|release) ;; *) exit 2 ;; esac

clean_source() {
  bash tools/fountain-check-clean.sh
}
clean_source
[[ $(uname -sm) == 'Linux x86_64' ]]
export GOTOOLCHAIN=go1.26.9 GOWORK=off
[[ $(go version) == 'go version go1.26.9 linux/amd64' ]]
[[ $(node --version) == "v$(<.node-version)" ]]
[[ "npm@$(npm --version)" == $(node -p "require('./package.json').packageManager") ]]
[[ ${TMPDIR:-} == /home/* && -d $TMPDIR ]]
export CI=true TZ=UTC
case $1 in source|release) export GOFLAGS='' ;; *) export GOFLAGS=-mod=readonly ;; esac
mkdir -p .artifacts/fountain-ci
if [[ -d node_modules ]]; then touch node_modules; fi

module_checks() {
  go mod verify
  for module in sdk third_party/go-chi-session; do
    (
      cd "$module"
      go mod verify
      go mod tidy
      if [[ $module == sdk ]]; then go vet ./...; else go vet .; fi
    )
  done
  # The upstream formatter omits independent modules. Format only our SDK and
  # the patched session files, preserving the unchanged vendored providers.
  mapfile -d '' sdk_files < <(git ls-files -z 'sdk/*.go' 'sdk/**/*.go' 'third_party/go-chi-session/file.go' 'third_party/go-chi-session/file_lifecycle_test.go')
  if [[ ${#sdk_files[@]} != 0 ]]; then
    go run mvdan.cc/gofumpt@v0.9.2 -w "${sdk_files[@]}"
  fi
}

case $1 in
  source)
    make --what-if=routers/api/v1/api.go checks lint-backend-analysis lint-swagger
    module_checks
    ;;
  unit)
    make test-backend GOTESTFLAGS=-count=1
    (cd sdk && go test -count=1 ./...)
    go test -race -count=1 -tags 'sqlite sqlite_unlock_notify' \
      ./models/nativeoperation ./services/nativeoperation ./services/extensions ./modules/session ./modules/queue ./routers/common \
      ./services/pull ./services/actions ./services/repository \
      code.forgejo.org/go-chi/session
    ;;
  database)
    : "${FORGEJO_TEST_PG_DSN:?Set a DSN for a disposable PostgreSQL database}"
    make backend TAGS='sqlite sqlite_unlock_notify'
    make test-sqlite-migration test-sqlite GOTESTFLAGS=-count=1 GOTESTCOMPILEDRUNSUFFIX=-test.timeout=70m
    # Each package mutates schema/fixtures. Never run these against one database
    # concurrently; pipefail preserves go test failures before JSON admission.
    for package in models/nativeoperation services/nativeoperation models/forgejo_migrations; do
      result=".artifacts/fountain-ci/pg-${package//\//-}.jsonl"
      GITEA_ROOT="$PWD" GITEA_CONF=tests/sqlite.ini go test -json -count=1 \
        -tags 'sqlite sqlite_unlock_notify' -run 'OnPostgres$' "./$package" | tee "$result"
      node tools/fountain-test-results.mjs "$result"
    done
    ;;
  browser)
    make lint-frontend test-frontend
    npx --no-install tsc -p sdk/browser/tsconfig.json
    make frontend backend TAGS='sqlite sqlite_unlock_notify'
    go build -trimpath -o .artifacts/fountain-ci/example-backend ./contrib/extensions/example
    export RUN_ALL=1 PLAYWRIGHT_PROJECT=chromium PLAYWRIGHT_FLAGS=chromium
    make 'test-e2e-sqlite#fountain' GOTESTFLAGS=-count=1
    ;;
  packaging)
    for script in tools/fountain-*.sh; do bash -n "$script"; done
    hadolint --failure-threshold warning Dockerfile
    go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/fountain-ci.yml
    ;;
  release)
    # The local release profile shares existing source commands. PostgreSQL and
    # real-browser qualification are the hosted workflow's separate lanes.
    make --what-if=routers/api/v1/api.go checks lint-frontend test-frontend
    npx --no-install tsc -p sdk/browser/tsconfig.json
    go test -count=1 -tags 'sqlite sqlite_unlock_notify' \
      ./modules/extensions ./modules/setting ./services/extensions ./cmd ./contrib/extensions/example
    (cd sdk && go test -count=1 ./...)
    ;;
esac
clean_source
