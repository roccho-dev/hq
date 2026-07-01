from __future__ import annotations

from dataclasses import asdict, dataclass
from typing import Any, Mapping

from .core import CursorContext, JsonlWorld


@dataclass(frozen=True)
class Suggestion:
    label: str
    detail: str
    edit: Mapping[str, Any]
    meaning: Mapping[str, Any]
    compileDraft: Mapping[str, Any]

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


def suggest_keys(world: JsonlWorld, context: CursorContext) -> tuple[Suggestion, ...]:
    if context.state not in {"empty", "object_open", "key", "next_key", "object"}:
        return ()

    present = set(context.present_keys)
    required_missing = [key for key in world.required_keys if key not in present]
    ordered_keys = required_missing + [key for key in world.schema_keys if key not in required_missing]

    result: list[Suggestion] = []
    for key in ordered_keys:
        if key in present:
            continue
        if context.partial and not key.startswith(context.partial):
            continue
        result.append(key_suggestion(key, context))
    return tuple(result)


def key_suggestion(key: str, context: CursorContext) -> Suggestion:
    text = chr(34) + key + chr(34) + ": "
    return Suggestion(
        label=key,
        detail="insert JSONL key",
        edit={"kind": "insert_key", "key": key, "text": text, "replacePartial": context.partial},
        meaning={"kind": "jsonl.key", "key": key, "cursorState": context.state},
        compileDraft={"op": "set_key", "key": key, "cursorState": context.state},
    )
