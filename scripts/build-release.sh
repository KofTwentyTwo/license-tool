#!/usr/bin/env bash
# Cross-compile the release archives into dist/ (the Gitea CI runs this on v* tags).
#
# Same targets, names and ldflags as .goreleaser.yaml: darwin/linux amd64+arm64 and
# windows amd64; tar.gz everywhere except Windows, which gets a zip.
#
# Usage: scripts/build-release.sh <version>   (for example 1.2.3 or 1.2.3-rc.1)
set -euo pipefail

version=${1:?usage: scripts/build-release.sh <version>}
commit=$(git rev-parse HEAD)
date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ldflags="-s -w -X main.version=$version -X main.commit=$commit -X main.date=$date"

cd "$(dirname "${BASH_SOURCE[0]}")/.."
rm -rf dist && mkdir dist
for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64; do
   os=${target%/*}
   arch=${target#*/}
   name=license-tool_${version}_${os}_${arch}
   ext=
   [[ $os == windows ]] && ext=.exe
   mkdir "dist/$name"
   CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$ldflags" \
      -o "dist/$name/license-tool$ext" ./cmd/license-tool
   cp README.md LICENSE CHANGELOG.md "dist/$name/"
   if [[ $os == windows ]]; then
      (cd "dist/$name" && python3 -m zipfile -c "../$name.zip" ./*)
   else
      tar -czf "dist/$name.tar.gz" -C "dist/$name" .
   fi
   rm -r "dist/${name:?}"
done
ls -l dist
