#!/bin/sh
set -eu
cd "$(dirname "$0")"
export CGO_ENABLED=0
go test ./...
go build -buildvcs=false -trimpath -ldflags='-s -w' -o folder2iso .
