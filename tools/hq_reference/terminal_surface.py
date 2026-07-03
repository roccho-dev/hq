from __future__ import annotations

import json
from typing import Any

from .core import JsonlWorld, derive_cursor_context
from .finalize import finalize_selection
from .suggestion import diagnose_keys, suggest_all


def autocomplete_payload(schema: dict[str, Any], rows_jsonl: str, buffer: str, position: int | None = None) -> dict[str, Any]:
    world = JsonlWorld.from_jsonl_text(schema, rows_jsonl)
    context = derive_cursor_context(buffer, position)
    return {
        "context": {
            "state": context.state,
            "partial": context.partial,
            "presentKeys": list(context.present_keys),
            "currentKey": context.current_key,
            "missingRequired": list(context.missing_required_keys_for(world)),
        },
        "diagnostics": [item.to_dict() for item in diagnose_keys(world, context)],
        "suggestions": [item.to_dict() for item in suggest_all(world, context)],
    }


def complete_payload(schema: dict[str, Any], rows_jsonl: str, buffer: str) -> list[dict[str, Any]]:
    return autocomplete_payload(schema, rows_jsonl, buffer)["suggestions"]


def choose_payload(schema: dict[str, Any], rows_jsonl: str, buffer: str, index: int) -> dict[str, Any]:
    world = JsonlWorld.from_jsonl_text(schema, rows_jsonl)
    context = derive_cursor_context(buffer)
    candidates = suggest_all(world, context)
    return finalize_selection(candidates, index)


def preview_text(schema: dict[str, Any], rows_jsonl: str) -> str:
    payload = autocomplete_payload(schema, rows_jsonl, "{")
    return json.dumps(payload, ensure_ascii=False, sort_keys=True)
