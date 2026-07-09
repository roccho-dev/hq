#!/usr/bin/env bash
set -euo pipefail

mkdir -p dist artifacts

go test ./...
go build -o dist/hq-linux-amd64 ./cmd/hq
GOOS=windows GOARCH=amd64 go build -o dist/hq-windows-amd64.exe ./cmd/hq

./dist/hq-linux-amd64 --complete '{"op":q' >/tmp/hq-complete.json
./dist/hq-linux-amd64 --context '{"' >/tmp/hq-context.json
./dist/hq-linux-amd64 --draft '{"op":"queue.create","target":"ctx","payload":{"path":"demo.jsonl"}}' >/tmp/hq-draft.json

ACCEPT_ARG='{"op":"queue.create","target":"ctx","payload":{"path":"demo.jsonl"}}'
QUEUE=/tmp/hq-accept.queue.jsonl
DRAFT_QUEUE=/tmp/hq-draft-noqueue.jsonl
rm -f "$QUEUE" "$DRAFT_QUEUE" /tmp/hq-accept.json /tmp/hq-draft-noqueue.json
./dist/hq-linux-amd64 --accept "$ACCEPT_ARG" --queue "$QUEUE" >/tmp/hq-accept.json
./dist/hq-linux-amd64 --draft "$ACCEPT_ARG" --queue "$DRAFT_QUEUE" >/tmp/hq-draft-noqueue.json

python3 - <<'PY'
import json
from pathlib import Path

complete = json.loads(Path('/tmp/hq-complete.json').read_text())
assert any(row.get('label') == 'queue.create' for row in complete), complete
assert any(row.get('compileDraft', {}).get('queue') == 'instruction.jsonl' for row in complete), complete

context = json.loads(Path('/tmp/hq-context.json').read_text())
assert context['kind'] == 'key', context

accept = json.loads(Path('/tmp/hq-accept.json').read_text())
assert accept['kind'] == 'accepted.instruction', accept
assert accept['queue'] == 'instruction.jsonl', accept

queue_rows = Path('/tmp/hq-accept.queue.jsonl').read_text().splitlines()
assert len(queue_rows) == 1, queue_rows
queued = json.loads(queue_rows[0])
assert queued == accept, (queued, accept)

# A draft preview must not append even when a queue path is supplied.
assert not Path('/tmp/hq-draft-noqueue.jsonl').exists()
PY

python3 scripts/vim-hq-contract-proof.py \
  --binary ./dist/hq-linux-amd64 \
  --artifact artifacts/vim-hq-contract-proof-linux.json

printf 'ok\n'
