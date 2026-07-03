#!/usr/bin/env bash
set -euo pipefail

go test ./...
go build -o dist/hq-reflective-linux-amd64 ./cmd/hq-reflective
GOOS=windows GOARCH=amd64 go build -o dist/hq-reflective-windows-amd64.exe ./cmd/hq-reflective
./dist/hq-reflective-linux-amd64 --complete '{"op":q' >/tmp/hq-reflective-complete.json
./dist/hq-reflective-linux-amd64 --context '{"' >/tmp/hq-reflective-context.json
./dist/hq-reflective-linux-amd64 --draft '{"op":"queue.create","target":"ctx","payload":{"path":"demo.jsonl"}}' >/tmp/hq-reflective-draft.json
printf 'ok\n'
