#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p dist artifacts

if ! go test ./... >artifacts/go-test.log 2>&1; then
  tail -n 40 artifacts/go-test.log >&2
  exit 1
fi
python3 -m unittest discover -s tests -p 'test_protocol_contract*.py'
go build -o dist/hq-linux-amd64 ./cmd/hq
GOOS=windows GOARCH=amd64 go build -o dist/hq-windows-amd64.exe ./cmd/hq

complete_out="$work/complete.json"
context_out="$work/context.json"
draft_out="$work/draft.json"
accept_out="$work/accept.json"
accept_no_queue_out="$work/accept-no-queue.json"
complete_queue="$work/complete.queue.jsonl"
context_queue="$work/context.queue.jsonl"
draft_queue="$work/draft.queue.jsonl"
accept_queue="$work/accept.queue.jsonl"
sentinel="$work/external-execution.marker"
sentinel_bin="$work/sentinel-bin"
mkdir -p "$sentinel_bin"

for tool in herdr codex claude sh pwsh powershell; do
  cat >"$sentinel_bin/$tool" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$0" >>"${HQ_EXEC_SENTINEL:?}"
exit 97
EOF
  chmod +x "$sentinel_bin/$tool"
done

original_path=$PATH
export HQ_EXEC_SENTINEL="$sentinel"
export PATH="$sentinel_bin:$PATH"

accept_arg='{"op":"queue.create","target":"ctx","payload":{"path":"demo.jsonl","requested_adapter":"codex"}}'

./dist/hq-linux-amd64 --complete '{"op":q' --queue "$complete_queue" >"$complete_out"
./dist/hq-linux-amd64 --context '{"' --queue "$context_queue" >"$context_out"
./dist/hq-linux-amd64 --draft "$accept_arg" --queue "$draft_queue" >"$draft_out"
./dist/hq-linux-amd64 --accept "$accept_arg" --queue "$accept_queue" >"$accept_out"
./dist/hq-linux-amd64 --accept "$accept_arg" >"$accept_no_queue_out"

COMPLETE_OUT="$complete_out" \
CONTEXT_OUT="$context_out" \
DRAFT_OUT="$draft_out" \
ACCEPT_OUT="$accept_out" \
ACCEPT_NO_QUEUE_OUT="$accept_no_queue_out" \
COMPLETE_QUEUE="$complete_queue" \
CONTEXT_QUEUE="$context_queue" \
DRAFT_QUEUE="$draft_queue" \
ACCEPT_QUEUE="$accept_queue" \
EXEC_SENTINEL="$sentinel" \
python3 - <<'PY'
import json
import os
from pathlib import Path

complete = json.loads(Path(os.environ["COMPLETE_OUT"]).read_text())
assert any(row.get("label") == "queue.create" for row in complete), complete
assert any(row.get("compileDraft", {}).get("queue") == "instruction.jsonl" for row in complete), complete

context = json.loads(Path(os.environ["CONTEXT_OUT"]).read_text())
assert context["kind"] == "key", context

draft = json.loads(Path(os.environ["DRAFT_OUT"]).read_text())
assert draft["kind"] == "accepted.instruction", draft

accept = json.loads(Path(os.environ["ACCEPT_OUT"]).read_text())
accept_without_queue = json.loads(Path(os.environ["ACCEPT_NO_QUEUE_OUT"]).read_text())
assert accept["kind"] == "accepted.instruction", accept
assert accept["queue"] == "instruction.jsonl", accept
assert accept_without_queue == accept, (accept_without_queue, accept)

for name in ("COMPLETE_QUEUE", "CONTEXT_QUEUE", "DRAFT_QUEUE"):
    assert not Path(os.environ[name]).exists(), (name, Path(os.environ[name]).read_text())

queue_rows = Path(os.environ["ACCEPT_QUEUE"]).read_text().splitlines()
assert len(queue_rows) == 1, queue_rows
queued = json.loads(queue_rows[0])
assert queued == accept, (queued, accept)

sentinel = Path(os.environ["EXEC_SENTINEL"])
assert not sentinel.exists(), sentinel.read_text() if sentinel.exists() else ""
PY

export PATH="$original_path"

python3 scripts/vim-hq-contract-proof.py \
  --binary ./dist/hq-linux-amd64 \
  --artifact artifacts/vim-hq-contract-proof-linux.json

printf 'ok\n'
