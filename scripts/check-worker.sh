#!/usr/bin/env bash
set -euo pipefail

mkdir -p artifacts/worker-core

go test ./internal/worker ./cmd/hq-worker
go test -race ./internal/worker
go vet ./internal/worker ./cmd/hq-worker
go build -o artifacts/worker-core/hq-worker-linux-amd64 ./cmd/hq-worker
GOOS=windows GOARCH=amd64 go build -o artifacts/worker-core/hq-worker-windows-amd64.exe ./cmd/hq-worker

QUEUE=internal/worker/testdata/valid.jsonl
EVENTS=artifacts/worker-core/events.jsonl
PLAN=artifacts/worker-core/plan.jsonl
NORMAL=artifacts/worker-core/normal.jsonl
DUPLICATE=artifacts/worker-core/duplicate.jsonl
rm -f "$EVENTS" "$PLAN" "$NORMAL" "$DUPLICATE"

./artifacts/worker-core/hq-worker-linux-amd64 \
  --input "$QUEUE" \
  --events "$EVENTS" \
  --workspace . \
  --dry-run >"$PLAN"

test ! -e "$EVENTS"

set +e
./artifacts/worker-core/hq-worker-linux-amd64 \
  --input "$QUEUE" \
  --events "$EVENTS" \
  --workspace . >"$NORMAL"
normal_rc=$?
set -e
test "$normal_rc" -eq 2

set +e
./artifacts/worker-core/hq-worker-linux-amd64 \
  --input "$QUEUE" \
  --events "$EVENTS" \
  --workspace . >"$DUPLICATE"
duplicate_rc=$?
set -e
test "$duplicate_rc" -eq 2

python3 - <<'PY'
import json
from pathlib import Path

root = Path("artifacts/worker-core")
plans = [json.loads(line) for line in (root / "plan.jsonl").read_text().splitlines() if line]
assert len(plans) == 4, plans
assert {row["target"] for row in plans} == {"sh", "herdr", "codex", "claude"}, plans
assert all(row["version"] == "worker.plan.v1" for row in plans), plans
assert all(row["decision"] == "accepted" and row["policy"]["allowed"] for row in plans), plans

normal = [json.loads(line) for line in (root / "normal.jsonl").read_text().splitlines() if line]
assert len(normal) == 8, normal
assert sum(row["version"] == "result.v1" and row["kind"] == "accepted" for row in normal) == 4, normal
blocked = [row for row in normal if row["version"] == "result.v1" and row["kind"] == "blocked"]
assert len(blocked) == 4, blocked
assert all(row["error"]["code"] == "adapter_unavailable" for row in blocked), blocked

duplicates = [json.loads(line) for line in (root / "duplicate.jsonl").read_text().splitlines() if line]
assert len(duplicates) == 4, duplicates
assert all(row["version"] == "validation.v1" and row["status"] == "blocked" and row["error"]["code"] == "duplicate_id" for row in duplicates), duplicates

evidence = [json.loads(line) for line in (root / "events.jsonl").read_text().splitlines() if line]
assert len(evidence) == 12, evidence
assert len({row.get("event_id") for row in evidence if row["version"] == "result.v1"}) == 8
assert sum(row["version"] == "validation.v1" for row in evidence) == 4
PY
