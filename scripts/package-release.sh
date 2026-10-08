#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

release_version=${1:-}
if [[ $# -ne 1 || ! "$release_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo 'Usage: bash scripts/package-release.sh v0.1.0' >&2
  exit 1
fi
release_dir="$PWD/dist/$release_version"
mkdir -p dist
mkdir "$release_dir" # Never replace existing release artifacts.
packaging_tmp=$(mktemp -d)
packaging_complete=0
cleanup() {
  rm -rf "$packaging_tmp"
  if [[ "$packaging_complete" == 0 ]]; then rm -rf "$release_dir"; fi
}
trap cleanup EXIT

notices="$packaging_tmp/THIRD_PARTY_LICENSES.txt"
printf 'Licenses of the Go runtime and modules linked into the pfw CLI.\n\n=== Go runtime ===\n' > "$notices"
cat "$(go env GOROOT)/LICENSE" >> "$notices"
go list -deps -f '{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}|{{.Module.Dir}}{{end}}{{end}}' ./cmd/pfw | sort -u > "$packaging_tmp/modules.txt"
while IFS='|' read -r module_name module_dir; do
  if [[ -z "$module_name" ]]; then continue; fi
  found_license=0
  for license_name in LICENSE LICENSE.txt LICENSE.md LICENCE LICENCE.txt LICENCE.md COPYING; do
    if [[ -f "$module_dir/$license_name" ]]; then
      printf '\n\n=== %s ===\n' "$module_name" >> "$notices"
      cat "$module_dir/$license_name" >> "$notices"
      found_license=1
      break
    fi
  done
  if [[ "$found_license" == 0 ]]; then
    printf 'Missing license for linked module: %s\n' "$module_name" >&2
    exit 1
  fi
done < "$packaging_tmp/modules.txt"

for release_os in darwin linux; do
  for release_arch in amd64 arm64; do
    binary=pfw
    CGO_ENABLED=0 GOOS="$release_os" GOARCH="$release_arch" go build \
      -trimpath -ldflags="-s -w -X main.version=$release_version" \
      -o "$packaging_tmp/$binary" ./cmd/pfw
    cp LICENSE README.md CHANGELOG.md "$packaging_tmp/"
    archive="pfw_${release_version}_${release_os}_${release_arch}"
    COPYFILE_DISABLE=1 tar -czf "$release_dir/$archive.tar.gz" -C "$packaging_tmp" "$binary" LICENSE README.md CHANGELOG.md THIRD_PARTY_LICENSES.txt
  done
done
(cd "$release_dir" && shasum -a 256 *.tar.gz > checksums.txt)
packaging_complete=1
printf 'Release artifacts: %s\n' "$release_dir"
