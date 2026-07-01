from __future__ import annotations

from dataclasses import asdict, dataclass
from difflib import get_close_matches
from typing import Any, Mapping

from .core import CursorContext, JsonlWorld


@dataclass(frozen=True)
class Suggestion:
    label: str
    detail: str
    edit: Mapping[str, Any]
    meaning: Mapping[str, Any]
    compileDraft: Mapping[str, Any]
    kind: str = "candidate"

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


@dataclass(frozen=True)
class Diagnostic:
    code: str
    message: str
    fixes: tuple[Suggestion, ...] = ()

    def to_dict(self) -> dict[str, Any]:
        return {
            "code": self.code,
            "message": self.message,
            "fixes": [fix.to_dict() for fix in self.fixes],
        }


def suggest_keys(world: JsonlWorld, context: CursorContext) -> tuple[Suggestion, ...]:
    if context.state not in {"empty", "object_open", "key", "next_key", "object"}:
        return ()

    present = set(context.present_keys)
    required_missing = list(context.missing_required_keys_for(world))
    ordered_keys = required_missing + [key for key in world.schema_keys if key not in required_missing]

    result: list[Suggestion] = []
    for key in ordered_keys:
        if key in present:
            continue
        if context.partial and not key.startswith(context.partial):
            continue
        result.append(key_suggestion(key, context, required=key in required_missing))
    return tuple(result)


def suggest_values(world: JsonlWorld, context: CursorContext) -> tuple[Suggestion, ...]:
    if context.state != "value" or not context.current_key:
        return ()
    prop = world.property_for(context.current_key)
    enum_values = prop.get("enum", ())
    if not isinstance(enum_values, (list, tuple)):
        return ()
    result: list[Suggestion] = []
    for value in enum_values:
        text_value = str(value)
        if context.partial and not text_value.startswith(context.partial):
            continue
        result.append(value_suggestion(context.current_key, text_value, context))
    return tuple(result)


def suggest_all(world: JsonlWorld, context: CursorContext) -> tuple[Suggestion, ...]:
    if context.state == "value":
        return suggest_values(world, context)
    return suggest_keys(world, context)


def diagnose_keys(world: JsonlWorld, context: CursorContext) -> tuple[Diagnostic, ...]:
    diagnostics: list[Diagnostic] = []
    schema_keys = set(world.schema_keys)

    if context.state == "key" and context.partial in context.present_keys:
        alternatives = tuple(key_suggestion(key, context, required=key in context.missing_required_keys_for(world)) for key in world.schema_keys if key not in context.present_keys)
        diagnostics.append(
            Diagnostic(
                code="duplicate_key",
                message=f"key already exists: {context.partial}",
                fixes=alternatives[:3],
            )
        )

    for key in context.duplicate_keys:
        diagnostics.append(Diagnostic(code="duplicate_key", message=f"key already exists: {key}"))

    for key in context.present_keys:
        if key in schema_keys:
            continue
        match = get_close_matches(key, world.schema_keys, n=1, cutoff=0.6)
        fixes = (rename_key_suggestion(key, match[0], context),) if match else ()
        diagnostics.append(
            Diagnostic(
                code="unknown_key",
                message=f"{key} is not in schema",
                fixes=fixes,
            )
        )

    return tuple(diagnostics)


def key_suggestion(key: str, context: CursorContext, required: bool = False) -> Suggestion:
    text = chr(34) + key + chr(34) + ": "
    return Suggestion(
        label=key,
        detail="required" if required else "optional",
        edit={"kind": "complete_key" if context.partial else "insert_key", "key": key, "text": text, "replacePartial": context.partial},
        meaning={"kind": "jsonl.key", "key": key, "cursorState": context.state},
        compileDraft={
            "op": "complete_key" if context.partial else "set_key",
            "key": key,
            "target": "current_object",
            "reason": "required_missing" if required else "schema_key",
        },
        kind="key",
    )


def value_suggestion(key: str, value: str, context: CursorContext) -> Suggestion:
    return Suggestion(
        label=value,
        detail="enum",
        edit={"kind": "set_value", "key": key, "value": value, "replacePartial": context.partial, "text": value + '"'},
        meaning={"kind": "jsonl.value", "key": key, "value": value, "cursorState": context.state},
        compileDraft={"op": "set_value", "key": key, "value": value, "target": "current_object"},
        kind="value",
    )


def rename_key_suggestion(from_key: str, to_key: str, context: CursorContext) -> Suggestion:
    return Suggestion(
        label=f"rename to {to_key}",
        detail="quick_fix",
        edit={"kind": "rename_key", "from": from_key, "to": to_key},
        meaning={"kind": "jsonl.rename_key", "from": from_key, "to": to_key, "cursorState": context.state},
        compileDraft={"op": "rename_key", "from": from_key, "to": to_key, "target": "current_object"},
        kind="quick_fix",
    )
