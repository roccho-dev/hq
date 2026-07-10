#!/usr/bin/env bash
set -euo pipefail

mkdir -p artifacts/worker-core

go test ./internal/worker ./cmd/hq-worker 2>&1 | tee artifacts/worker-core/go-test.log
go test -race ./internal/worker 2>&1 | tee artifacts/worker-core/go-race.log
go vet ./internal/worker ./cmd/hq-worker 2>&1 | tee artifacts/worker-core/go-vet.log
go build -o artifacts/worker-core/hq-worker-linux-amd64 ./cmd/hq-worker
GOOS=windows GOARCH=amd64 go build -o artifacts/worker-core/hq-worker-windows-amd64.exe ./cmd/hq-worker

QUEUE=internal/worker/testdata/valid.jsonl
EVENTS=artifacts/worker-core/events.jsonl
PLAN=artifacts/worker-core/plan.jsonl
NORMAL=artifacts/worker-core/normal.jsonl
DUPLICATE=artifacts/worker-core/duplicate.jsonl
LEDGER=artifacts/worker-core/ledger.jsonl
SHOW=artifacts/worker-core/show.json
TAIL=artifacts/worker-core/tail.jsonl
rm -f "$EVENTS" "$PLAN" "$NORMAL" "$DUPLICATE" "$LEDGER" "$SHOW" "$TAIL"

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

./artifacts/worker-core/hq-worker-linux-amd64 list \
  --input "$QUEUE" \
  --events "$EVENTS" \
  --json >"$LEDGER"

RUN_ID="$(python3 - <<'PY'
import json
from pathlib import Path
rows = [json.loads(line) for line in Path("artifacts/worker-core/ledger.jsonl").read_text().splitlines() if line]
assert rows, rows
print(rows[0]["run_id"])
PY
)"

./artifacts/worker-core/hq-worker-linux-amd64 show \
  --input "$QUEUE" \
  --events "$EVENTS" \
  --run "$RUN_ID" \
  --json >"$SHOW"

./artifacts/worker-core/hq-worker-linux-amd64 tail \
  --events "$EVENTS" \
  --run "$RUN_ID" \
  --follow=false \
  --json >"$TAIL"

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

ledger = [json.loads(line) for line in (root / "ledger.jsonl").read_text().splitlines() if line]
assert len(ledger) == 4, ledger
assert {row["target"] for row in ledger} == {"sh", "herdr", "codex", "claude"}, ledger
assert all(row["version"] == "worker.ledger.v1" for row in ledger), ledger
assert all(row["status"] == "blocked" and row["last_kind"] == "blocked" for row in ledger), ledger
assert len({row["summary"] for row in ledger}) == 4, ledger
for previous, current in zip(ledger, ledger[1:]):
    assert previous["last_event_at"] > current["last_event_at"] or (
        previous["last_event_at"] == current["last_event_at"]
        and previous["run_id"] < current["run_id"]
    ), ledger

show = json.loads((root / "show.json").read_text())
assert show["version"] == "worker.run-detail.v1", show
assert show["run"]["run_id"] == ledger[0]["run_id"], show
assert show["run"]["status"] == "blocked", show
assert show["run"]["error"]["code"] == "adapter_unavailable", show
assert "error" not in show, show
assert len(show["events"]) == 2, show

tail = [json.loads(line) for line in (root / "tail.jsonl").read_text().splitlines() if line]
assert [row["kind"] for row in tail] == ["accepted", "blocked"], tail
assert tail[-1]["run_id"] == ledger[0]["run_id"], tail
PY
