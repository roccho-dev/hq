from __future__ import annotations

from dataclasses import dataclass
import json
from typing import Any, Iterable, Mapping

from .core import JsonlWorld

_ALLOWED_TARGETS = {"ui.overlay", "work.dispatch", "adrs.proposal", "receipt.close"}
_REQUIRED_QUEUE_FIELDS = ("id", "target", "op", "source_ref", "payload", "status", "provenance", "created_at")


@dataclass(frozen=True)
class QueueRow:
    id: str
    target: str
    op: str
    source_ref: str
    payload: Mapping[str, Any]
    status: str
    provenance: Mapping[str, Any]
    created_at: str

    def to_dict(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "target": self.target,
            "op": self.op,
            "source_ref": self.source_ref,
            "payload": dict(self.payload),
            "status": self.status,
            "provenance": dict(self.provenance),
            "created_at": self.created_at,
        }


def parse_jsonl(text: str) -> tuple[dict[str, Any], ...]:
    rows: list[dict[str, Any]] = []
    for line_no, line in enumerate(text.splitlines(), 1):
        if not line.strip():
            continue
        value = json.loads(line)
        if not isinstance(value, dict):
            raise ValueError(f"line {line_no}: JSONL row must be an object")
        rows.append(value)
    return tuple(rows)


def validate_queue_row(row: Mapping[str, Any]) -> QueueRow:
    missing = [field for field in _REQUIRED_QUEUE_FIELDS if field not in row]
    if missing:
        raise ValueError("queue row missing fields: " + ",".join(missing))
    if row["target"] not in _ALLOWED_TARGETS:
        raise ValueError("queue row target is not allowed")
    if not str(row["source_ref"]):
        raise ValueError("queue row source_ref must be opaque and non-empty")
    if row["status"] != "pending":
        raise ValueError("hq may only create pending queue rows")
    if not isinstance(row["payload"], Mapping):
        raise ValueError("queue row payload must be an object")
    if not isinstance(row["provenance"], Mapping):
        raise ValueError("queue row provenance must be an object")
    return QueueRow(
        id=str(row["id"]),
        target=str(row["target"]),
        op=str(row["op"]),
        source_ref=str(row["source_ref"]),
        payload=dict(row["payload"]),
        status=str(row["status"]),
        provenance=dict(row["provenance"]),
        created_at=str(row["created_at"]),
    )


def read_queue_jsonl(text: str) -> tuple[QueueRow, ...]:
    return tuple(validate_queue_row(row) for row in parse_jsonl(text))


def encode_queue_row(row: QueueRow) -> str:
    return json.dumps(row.to_dict(), ensure_ascii=False, sort_keys=True, separators=(",", ":"))


def append_if_accepted(existing_text: str, accepted: bool, row: QueueRow) -> str:
    if not accepted:
        return existing_text
    prefix = existing_text
    if prefix and not prefix.endswith("\n"):
        prefix += "\n"
    return prefix + encode_queue_row(row) + "\n"


def projected_to_world(projected_text: str) -> JsonlWorld:
    rows = parse_jsonl(projected_text)
    properties: dict[str, dict[str, Any]] = {}
    required: list[str] = []
    for row in rows:
        for field in ("source_ref", "kind", "key", "title", "payload"):
            if field not in row:
                raise ValueError("projected row missing field: " + field)
        key = str(row["key"])
        payload = row["payload"] if isinstance(row["payload"], Mapping) else {}
        properties[key] = {"description": row.get("title", ""), **dict(payload)}
        if payload.get("required") is True:
            required.append(key)
    schema = {"type": "object", "required": required, "properties": properties}
    return JsonlWorld.from_schema_and_rows(schema, rows)


def adapt_target_payload(row: QueueRow) -> dict[str, Any]:
    payload = dict(row.payload)
    if row.target == "ui.overlay":
        return {"target": row.target, "state_store": False, "payload": payload}
    if row.target == "work.dispatch":
        return {"target": row.target, "admitted": True, "payload": payload}
    if row.target == "adrs.proposal":
        return {"target": row.target, "authority": "pending", "payload": payload}
    if row.target == "receipt.close":
        return {"target": row.target, "receipt": payload}
    raise ValueError("unsupported target")


def effective_projection(projected_text: str, queue_text: str) -> tuple[dict[str, Any], ...]:
    fixed = list(parse_jsonl(projected_text))
    effective = [dict(row) | {"projection_layer": "fixed"} for row in fixed]
    for row in read_queue_jsonl(queue_text):
        effective.append({
            "projection_layer": "queue",
            "queue_id": row.id,
            "source_ref": row.source_ref,
            "target": row.target,
            "op": row.op,
            "payload": adapt_target_payload(row),
            "status": row.status,
        })
    return tuple(effective)
