#!/usr/bin/env bash
set -euo pipefail

mkdir -p dist
go test ./...
CGO_ENABLED=1 go build \
  -buildmode=c-shared \
  -trimpath \
  -ldflags="-s -w" \
  -o dist/xai-autoban.so \
  .

echo "Built dist/xai-autoban.so"
