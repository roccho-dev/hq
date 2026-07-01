from __future__ import annotations

import json
from typing import Any, Iterable, Mapping

from .suggestion import Suggestion


def finalize_selection(options: Iterable[Suggestion], selected_index: int) -> dict[str, Any]:
    items = tuple(options)
    if selected_index < 0 or selected_index >= len(items):
        raise IndexError("selected_index is outside options")
    selected = items[selected_index]
    return {
        "type": "jsonl.instruction",
        "label": selected.label,
        "edit": dict(selected.edit),
        "meaning": dict(selected.meaning),
        "compileDraft": dict(selected.compileDraft),
    }


def append_instruction_jsonl(existing_text: str, instruction: Mapping[str, Any]) -> str:
    row = json.dumps(dict(instruction), ensure_ascii=False, sort_keys=True)
    if existing_text and not existing_text.endswith("\n"):
        existing_text += "\n"
    return existing_text + row + "\n"
