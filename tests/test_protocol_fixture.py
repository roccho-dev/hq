import json
from pathlib import Path


def test_protocol_fixture_rows_are_queue_drafts():
    path = Path("spec/fixtures/protocol.keys.jsonl")
    rows = [json.loads(line) for line in path.read_text().splitlines() if line.strip()]
    assert rows
    assert all(row["expectedQueue"] == "instruction.jsonl" for row in rows)
    assert any(row.get("expectedKey") == "op" for row in rows)
