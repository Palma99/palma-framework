#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

formatting=$(find . -type f -name '*.go' -not -path './.git/*' -not -path './dist/*' -exec gofmt -l {} +)
if [[ -n "$formatting" ]]; then
  printf 'Run gofmt on:\n%s\n' "$formatting" >&2
  exit 1
fi

verification_tmp=$(mktemp -d)
trap 'rm -rf "$verification_tmp"' EXIT
cp go.mod go.sum "$verification_tmp/"
go mod tidy
cmp go.mod "$verification_tmp/go.mod"
cmp go.sum "$verification_tmp/go.sum"
go mod verify
go run ./cmd/pfw generate -env local -check ./examples/httpapi/internal/bootstrap
go run ./cmd/pfw generate -check ./examples/cleanup
go vet ./...
go test -race ./...
go build ./...
