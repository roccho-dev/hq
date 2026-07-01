from __future__ import annotations

import json
from typing import Any

from .core import JsonlWorld, derive_cursor_context
from .finalize import finalize_selection
from .suggestion import suggest_keys


def complete_payload(schema: dict[str, Any], rows_jsonl: str, buffer: str) -> list[dict[str, Any]]:
    world = JsonlWorld.from_jsonl_text(schema, rows_jsonl)
    context = derive_cursor_context(buffer)
    return [item.to_dict() for item in suggest_keys(world, context)]


def select_payload(schema: dict[str, Any], rows_jsonl: str, buffer: str, index: int) -> dict[str, Any]:
    world = JsonlWorld.from_jsonl_text(schema, rows_jsonl)
    context = derive_cursor_context(buffer)
    options = suggest_keys(world, context)
    return finalize_selection(options, index)


def preview_text(schema: dict[str, Any], rows_jsonl: str) -> str:
    payload = complete_payload(schema, rows_jsonl, "{")
    return json.dumps(payload, ensure_ascii=False, sort_keys=True)
