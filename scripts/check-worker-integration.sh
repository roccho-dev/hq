#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

artifact_root=artifacts/worker-integration
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$artifact_root"

go test ./internal/workeraccept ./internal/workerclaim ./internal/worker ./cmd/hq-worker 2>&1 | tee "$artifact_root/go-test.log"
go build -o "$artifact_root/hq-linux-amd64" ./cmd/hq
go build -o "$artifact_root/hq-worker-linux-amd64" ./cmd/hq-worker
GOOS=windows GOARCH=amd64 go build -o "$artifact_root/hq-windows-amd64.exe" ./cmd/hq
GOOS=windows GOARCH=amd64 go build -o "$artifact_root/hq-worker-windows-amd64.exe" ./cmd/hq-worker

instruction='{"id":"ins-actual-hq-001","version":"instruction.v1","op":"run","target":"sh","payload":{"argv":["printf","actual-hq"],"cwd":"."},"created_at":"2026-07-10T06:30:00Z","reason":"binary-level accepted bridge proof","labels":["m","integration"]}'
accepted="$work/accepted.jsonl"
events="$work/project/.hq/events/events.jsonl"
plan="$artifact_root/actual-hq-plan.jsonl"
normal="$artifact_root/actual-hq-normal.jsonl"
ledger="$artifact_root/actual-hq-ledger.jsonl"
show="$artifact_root/actual-hq-show.json"
mkdir -p "$work/project"

"$artifact_root/hq-linux-amd64" --accept "$instruction" --queue "$accepted" >"$artifact_root/actual-hq-accepted.json"

python3 - "$artifact_root/actual-hq-accepted.json" "$accepted" "$instruction" <<'PY'
import json
import sys
from pathlib import Path

returned = json.loads(Path(sys.argv[1]).read_text())
rows = [json.loads(line) for line in Path(sys.argv[2]).read_text().splitlines() if line]
original = json.loads(sys.argv[3])
assert returned["kind"] == "accepted.instruction", returned
assert returned["queue"] == "instruction.jsonl", returned
assert len(rows) == 1 and rows[0] == returned, rows
assert returned["instruction"] == original, (returned["instruction"], original)
PY

"$artifact_root/hq-worker-linux-amd64" \
  --input "$accepted" \
  --input-format accepted.instruction \
  --events "$events" \
  --workspace "$work/project" \
  --dry-run >"$plan"

test ! -e "$events"

set +e
"$artifact_root/hq-worker-linux-amd64" \
  --input "$accepted" \
  --input-format accepted.instruction \
  --events "$events" \
  --workspace "$work/project" >"$normal"
normal_rc=$?
set -e
test "$normal_rc" -eq 2
test ! -e "$work/project/.hq/worker/claim.json"

"$artifact_root/hq-worker-linux-amd64" list \
  --input "$accepted" \
  --input-format accepted.instruction \
  --events "$events" \
  --json >"$ledger"

run_id=$(python3 - "$ledger" <<'PY'
import json
import sys
from pathlib import Path
rows = [json.loads(line) for line in Path(sys.argv[1]).read_text().splitlines() if line]
assert len(rows) == 1, rows
print(rows[0]["run_id"])
PY
)

"$artifact_root/hq-worker-linux-amd64" show \
  --input "$accepted" \
  --input-format accepted.instruction \
  --events "$events" \
  --run "$run_id" \
  --json >"$show"

python3 - "$plan" "$normal" "$events" "$ledger" "$show" <<'PY'
import json
import sys
from pathlib import Path

plan = [json.loads(line) for line in Path(sys.argv[1]).read_text().splitlines() if line]
normal = [json.loads(line) for line in Path(sys.argv[2]).read_text().splitlines() if line]
evidence = [json.loads(line) for line in Path(sys.argv[3]).read_text().splitlines() if line]
ledger = [json.loads(line) for line in Path(sys.argv[4]).read_text().splitlines() if line]
show = json.loads(Path(sys.argv[5]).read_text())

assert len(plan) == 1 and plan[0]["instruction_id"] == "ins-actual-hq-001", plan
assert plan[0]["decision"] == "accepted", plan
assert plan[0]["instruction_digest"].startswith("sha256:") and len(plan[0]["instruction_digest"]) == 71, plan
assert [row.get("kind") for row in normal] == ["worker.policy.v1", "accepted", "blocked"], normal
assert normal[0]["status"] == "approval_required" and normal[0]["may_dispatch"] is False, normal
assert normal[-1]["error"]["code"] == "approval_required", normal
assert evidence == normal, (evidence, normal)
assert len(ledger) == 1 and ledger[0]["status"] == "blocked", ledger
assert show["run"]["instruction_id"] == "ins-actual-hq-001", show
assert show["run"]["error"]["code"] == "approval_required", show
PY

cp "$events" "$artifact_root/actual-hq-events.jsonl"
printf 'worker integration proof passed\n' | tee "$artifact_root/result.txt"
