#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

artifact_root=artifacts/directexec
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
rm -rf "$artifact_root"
mkdir -p "$artifact_root" "$work/project/.hq/events"

go test -count=1 -v ./internal/worker/directexec ./internal/worker 2>&1 | tee "$artifact_root/go-test.log"
go test -count=1 -race ./internal/worker/directexec ./internal/worker 2>&1 | tee "$artifact_root/go-race.log"
go vet ./internal/worker/directexec ./internal/worker ./cmd/hq-worker ./cmd/hq-worker-fixture 2>&1 | tee "$artifact_root/go-vet.log"
go build -o "$artifact_root/hq-worker-linux-amd64" ./cmd/hq-worker
go build -o "$artifact_root/hq-worker-fixture-linux-amd64" ./cmd/hq-worker-fixture
GOOS=windows GOARCH=amd64 go build -o "$artifact_root/hq-worker-windows-amd64.exe" ./cmd/hq-worker
GOOS=windows GOARCH=amd64 go build -o "$artifact_root/hq-worker-fixture-windows-amd64.exe" ./cmd/hq-worker-fixture

helper=$(cd "$artifact_root" && pwd)/hq-worker-fixture-linux-amd64
queue="$work/instructions.jsonl"
approvals="$work/approvals.jsonl"
events="$work/project/.hq/events/events.jsonl"
sentinel="$work/project/sentinel.txt"
args_file="$work/project/args.json"
plan="$artifact_root/plan.jsonl"
normal="$artifact_root/normal.jsonl"
duplicate="$artifact_root/duplicate.jsonl"
ledger="$artifact_root/ledger.jsonl"
show="$artifact_root/show.json"
tail="$artifact_root/tail.jsonl"
secret="sk-aaaaaaaaaaaaaaaaaaaaaaaa"

python3 - "$queue" "$helper" "$sentinel" "$args_file" "$secret" <<'PY'
import json
import sys
from pathlib import Path

queue, helper, sentinel, args_file, secret = sys.argv[1:]
row = {
    "id": "ins-directexec-proof-001",
    "version": "instruction.v1",
    "op": "run",
    "target": "sh",
    "payload": {
        "argv": [
            helper,
            "--stdout", "token=" + secret,
            "--stderr", "direct-warning",
            "--sentinel", sentinel,
            "--args-file", args_file,
            "--",
            ";", "|", ">", "*", "$HOME", "%PATH%", '"quoted"', "space value",
        ],
        "cwd": ".",
    },
    "created_at": "2026-07-10T07:30:00Z",
    "reason": "G direct executable built-binary proof",
    "labels": ["g", "directexec", "proof"],
}
Path(queue).write_text(json.dumps(row, separators=(",", ":")) + "\n")
PY

"$artifact_root/hq-worker-linux-amd64" \
  --input "$queue" \
  --events "$events" \
  --workspace "$work/project" \
  --dry-run >"$plan"

test ! -e "$events"

python3 - "$plan" "$approvals" <<'PY'
import json
import sys
from pathlib import Path

plan = [json.loads(line) for line in Path(sys.argv[1]).read_text().splitlines() if line]
assert len(plan) == 1, plan
approval = {
    "version": "worker.approval.v1",
    "instruction_id": plan[0]["instruction_id"],
    "approved": True,
    "approved_by": "g-proof",
    "instruction_digest": plan[0]["instruction_digest"],
}
Path(sys.argv[2]).write_text(json.dumps(approval, separators=(",", ":")) + "\n")
PY

"$artifact_root/hq-worker-linux-amd64" \
  --input "$queue" \
  --events "$events" \
  --approvals "$approvals" \
  --workspace "$work/project" \
  --worker-id g-proof >"$normal"

test ! -e "$work/project/.hq/worker/claim.json"

set +e
"$artifact_root/hq-worker-linux-amd64" \
  --input "$queue" \
  --events "$events" \
  --approvals "$approvals" \
  --workspace "$work/project" \
  --worker-id g-proof-duplicate >"$duplicate"
duplicate_rc=$?
set -e
test "$duplicate_rc" -eq 2
test ! -e "$work/project/.hq/worker/claim.json"

"$artifact_root/hq-worker-linux-amd64" list --input "$queue" --events "$events" --json >"$ledger"
run_id=$(python3 - "$ledger" <<'PY'
import json
import sys
from pathlib import Path
rows = [json.loads(line) for line in Path(sys.argv[1]).read_text().splitlines() if line]
assert len(rows) == 1, rows
print(rows[0]["run_id"])
PY
)
"$artifact_root/hq-worker-linux-amd64" show --input "$queue" --events "$events" --run "$run_id" --json >"$show"
"$artifact_root/hq-worker-linux-amd64" tail --events "$events" --run "$run_id" --follow=false --json >"$tail"

python3 - "$plan" "$normal" "$duplicate" "$events" "$ledger" "$show" "$tail" "$sentinel" "$args_file" "$secret" <<'PY'
import json
import sys
from pathlib import Path

plan, normal, duplicate, events, ledger, show, tail, sentinel, args_file, secret = map(Path, sys.argv[1:10]) + [sys.argv[10]] if False else (None,)*11
PY

python3 - "$plan" "$normal" "$duplicate" "$events" "$ledger" "$show" "$tail" "$sentinel" "$args_file" "$secret" <<'PY'
import json
import sys
from pathlib import Path

plan_path, normal_path, duplicate_path, events_path, ledger_path, show_path, tail_path, sentinel_path, args_path = map(Path, sys.argv[1:10])
secret = sys.argv[10]
plan = [json.loads(line) for line in plan_path.read_text().splitlines() if line]
normal = [json.loads(line) for line in normal_path.read_text().splitlines() if line]
duplicate = [json.loads(line) for line in duplicate_path.read_text().splitlines() if line]
evidence = [json.loads(line) for line in events_path.read_text().splitlines() if line]
ledger = [json.loads(line) for line in ledger_path.read_text().splitlines() if line]
show = json.loads(show_path.read_text())
tail = [json.loads(line) for line in tail_path.read_text().splitlines() if line]
args = json.loads(args_path.read_text())

assert len(plan) == 1 and plan[0]["decision"] == "accepted", plan
assert [row.get("kind") for row in normal] == [
    "worker.policy.v1", "accepted", "started", "stdout", "stderr", "completed"
], normal
assert normal[0]["may_dispatch"] is True and normal[0]["status"] == "allowed", normal[0]
assert normal[-1]["final"]["text"] == "process exited successfully with status 0", normal[-1]
assert len(duplicate) == 1 and duplicate[0]["version"] == "validation.v1" and duplicate[0]["error"]["code"] == "duplicate_id", duplicate
assert [row.get("kind") for row in evidence if row.get("version") == "result.v1"] == [
    "accepted", "started", "stdout", "stderr", "completed"
], evidence
assert sum(row.get("version") == "validation.v1" for row in evidence) == 1, evidence
assert secret not in normal_path.read_text() and secret not in events_path.read_text(), "secret survived durable/output evidence"
assert sentinel_path.read_text() == "started\n", sentinel_path.read_text()
assert args == [";", "|", ">", "*", "$HOME", "%PATH%", '"quoted"', "space value"], args
assert len(ledger) == 1 and ledger[0]["status"] == "completed", ledger
assert show["run"]["status"] == "completed" and show["run"]["final"]["text"] == "process exited successfully with status 0", show
assert [row["kind"] for row in tail] == ["accepted", "started", "stdout", "stderr", "completed"], tail
PY

cp "$queue" "$artifact_root/instructions.jsonl"
cp "$approvals" "$artifact_root/approvals.jsonl"
cp "$events" "$artifact_root/events.jsonl"
cp "$sentinel" "$artifact_root/sentinel.txt"
cp "$args_file" "$artifact_root/args.json"
printf 'direct executable proof passed\n' | tee "$artifact_root/result.txt"
