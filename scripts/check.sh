#!/usr/bin/env bash
set -euo pipefail

go test ./...
go build -o dist/hq-linux-amd64 ./cmd/hq
GOOS=windows GOARCH=amd64 go build -o dist/hq-windows-amd64.exe ./cmd/hq
./dist/hq-linux-amd64 --complete '{"op":q' >/tmp/hq-complete.json
./dist/hq-linux-amd64 --context '{"' >/tmp/hq-context.json
./dist/hq-linux-amd64 --draft '{"op":"queue.create","target":"ctx","payload":{"path":"demo.jsonl"}}' >/tmp/hq-draft.json
printf 'ok\n'
