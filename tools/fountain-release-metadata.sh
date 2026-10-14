#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
set -euo pipefail
[[ $# == 5 ]]
out=$1 version=$2 commit=$3 image=$4 digest=$5
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+-fountain\.[1-9][0-9]*$ && $commit =~ ^[0-9a-f]{40}$ ]]
[[ $image =~ ^ghcr.io/[a-z0-9._/-]+$ && $digest =~ ^sha256:[0-9a-f]{64}$ ]]
(cd "$out" && sha256sum -c SHA256SUMS)
[[ $(gzip -dc "$out/fountain-$version-source.tar.gz" | { git get-tar-commit-id; cat >/dev/null; }) == "$commit" ]]
config=$(cat "$out/image-id")
[[ $config =~ ^sha256:[0-9a-f]{64}$ ]]
prepared="forgejo-src-$version+gitea-1.22.0.tar.gz"
[[ $(tar -xOf "$out/$prepared" "forgejo-src-$version+gitea-1.22.0/VERSION") == "$version+gitea-1.22.0" ]]
tar -tzf "$out/$prepared" | grep '/vendor/modules.txt$' > /dev/null
protocol=$(awk '$1 == "const" && $2 == "Protocol" && $3 == "=" {print $4}' "$out/source/sdk/manifest.go")
browser=$(awk '$1 == "BrowserContractVersion" && $2 == "=" {print $3}' "$out/source/sdk/contract.go")
[[ $protocol =~ ^[0-9]+$ && $browser =~ ^[0-9]+$ ]]
jq -n --arg version "$version" --arg commit "$commit" --arg image "$image@$digest" \
  --argjson protocol "$protocol" --argjson browser "$browser" \
  --arg config "$config" --arg source "fountain-$version-source.tar.gz" \
  --arg prepared "$prepared" \
  --arg prepared_sha256 "$(sha256sum "$out/$prepared" | cut -d' ' -f1)" \
  --arg source_sha256 "$(sha256sum "$out/fountain-$version-source.tar.gz" | cut -d' ' -f1)" \
  '{schema:1, version:$version, source_commit:$commit, source_archive:$source,
    source_sha256:$source_sha256, prepared_source:$prepared, prepared_source_sha256:$prepared_sha256,
    image:$image, image_config_digest:$config,
    platform:"linux/amd64", extension_protocol:$protocol, browser_contract:$browser}' > "$out/release.json"
(cd "$out" && sha256sum release.json >> SHA256SUMS && sha256sum -c SHA256SUMS)
