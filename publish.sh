#!/usr/bin/env bash
# publish.sh — build this fork and publish the npm/poc package to npm as
# @stavalfi/tsgolint, consumed by the poc monorepo through an npm alias.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$repo_root"

# `just init` minus the pnpm steps, which only the e2e suite and `just fmt` need.
# Guarded because `git am` is not idempotent.
if [[ ! -f typescript-go/go.mod ]]; then
  echo ">> initialising typescript-go submodule"
  # Only the pinned commit: the full typescript-go history makes the clone take minutes.
  git submodule update --init --depth 1
  (cd typescript-go && git am --3way --no-gpg-sign ../patches/*.patch)
  mkdir -p internal/collections
  find ./typescript-go/internal/collections -type f ! -name '*_test.go' \
    -exec cp {} internal/collections/ \;
fi

version="$(git describe --tags --abbrev=0 --match 'v*' | sed 's/^v//')-$(git rev-parse HEAD)"
tarball="stavalfi-tsgolint-${version}.tgz"

stage="$(mktemp -d)/package"
trap 'rm -rf "$(dirname "$stage")"' EXIT
cp -r npm/poc "$stage"
cp LICENSE README.md "$stage/"
npm --prefix "$stage" version "$version" --no-git-tag-version --allow-same-version >/dev/null

# Go cross-compiles cleanly here: tsgolint has no cgo.
export CGO_ENABLED=0
for target in darwin/arm64/darwin-arm64; do
  IFS=/ read -r goos goarch npm_platform <<<"$target"
  echo ">> building tsgolint for ${npm_platform}"
  GOOS="$goos" GOARCH="$goarch" go build \
    -ldflags="-s -w" -trimpath \
    -o "$stage/bin/tsgolint-${npm_platform}" ./cmd/tsgolint
done

out="$(dirname "$stage")/${tarball}"
tar -czf "$out" -C "$(dirname "$stage")" package
# Runs in .github/workflows/publish.yml: npm authenticates through trusted
# publishing (OIDC), so there is no token and no 2FA. From a laptop npm rejects
# it with 403. Every version is a `<tag>-<sha>` prerelease, which npm refuses
# without an explicit tag.
npm publish "$out" --access public --tag latest

echo
echo "poc package.json -> \"oxlint-tsgolint\": \"npm:@stavalfi/tsgolint@${version}\""
