from __future__ import annotations

from dataclasses import dataclass
import json
import re
from typing import Any, Iterable, Mapping


@dataclass(frozen=True)
class JsonlWorld:
    """Small read model for JSONL-backed input work."""

    schema_keys: tuple[str, ...]
    required_keys: tuple[str, ...]
    existing_keys: tuple[str, ...]
    rows: tuple[Mapping[str, Any], ...]
    properties: Mapping[str, Mapping[str, Any]]

    @classmethod
    def from_schema_and_rows(
        cls,
        schema: Mapping[str, Any] | None,
        rows: Iterable[Mapping[str, Any]] | None = None,
    ) -> "JsonlWorld":
        schema = schema or {}
        rows_tuple = tuple(rows or ())
        raw_properties = schema.get("properties", {})
        properties: dict[str, Mapping[str, Any]] = {}
        if isinstance(raw_properties, Mapping):
            for key, value in raw_properties.items():
                properties[str(key)] = value if isinstance(value, Mapping) else {}
        schema_keys = tuple(sorted(properties.keys()))
        required_keys = tuple(str(key) for key in schema.get("required", ()) if str(key) in schema_keys)
        existing = sorted({str(key) for row in rows_tuple for key in row.keys() if str(key) in schema_keys})
        return cls(
            schema_keys=schema_keys,
            required_keys=required_keys,
            existing_keys=tuple(existing),
            rows=rows_tuple,
            properties=properties,
        )

    @classmethod
    def from_jsonl_text(
        cls,
        schema: Mapping[str, Any] | None,
        jsonl_text: str,
    ) -> "JsonlWorld":
        rows: list[Mapping[str, Any]] = []
        for line in jsonl_text.splitlines():
            if not line.strip():
                continue
            value = json.loads(line)
            if not isinstance(value, dict):
                raise ValueError("JSONL rows must be objects")
            rows.append(value)
        return cls.from_schema_and_rows(schema, rows)

    def property_for(self, key: str) -> Mapping[str, Any]:
        return self.properties.get(key, {})


@dataclass(frozen=True)
class CursorContext:
    """Current buffer state, independent from terminal route names."""

    buffer: str
    position: int
    state: str
    partial: str
    present_keys: tuple[str, ...]
    current_key: str = ""
    duplicate_keys: tuple[str, ...] = ()

    def missing_required_keys_for(self, world: JsonlWorld) -> tuple[str, ...]:
        present = set(self.present_keys)
        return tuple(key for key in world.required_keys if key not in present)


_PRESENT_KEY = re.compile(r'"([^"\\]*(?:\\.[^"\\]*)*)"\s*:')
_OPEN_KEY = re.compile(r'(?:^|[\{,]\s*)"([^"\\]*)$')
_VALUE_AFTER_COLON = re.compile(r':\s*"?([^",}\s]*)$')


def _all_present_keys(buffer: str) -> tuple[str, ...]:
    return tuple(match.group(1) for match in _PRESENT_KEY.finditer(buffer))


def _present_keys(buffer: str) -> tuple[str, ...]:
    return tuple(dict.fromkeys(_all_present_keys(buffer)))


def _duplicate_keys(buffer: str) -> tuple[str, ...]:
    seen: set[str] = set()
    duplicates: list[str] = []
    for key in _all_present_keys(buffer):
        if key in seen and key not in duplicates:
            duplicates.append(key)
        seen.add(key)
    return tuple(duplicates)


def _last_present_key(buffer: str) -> str:
    keys = _all_present_keys(buffer)
    return keys[-1] if keys else ""


def derive_cursor_context(buffer: str, position: int | None = None) -> CursorContext:
    """Classify the editable position inside an in-progress JSON object."""

    if position is None:
        position = len(buffer)
    if position < 0 or position > len(buffer):
        raise ValueError("position must be inside the buffer")

    left = buffer[:position]
    stripped = left.strip()
    present = _present_keys(left)
    duplicates = _duplicate_keys(left)

    current_key = ""
    if not stripped:
        state = "empty"
        partial = ""
    elif match := _OPEN_KEY.search(left):
        state = "key"
        partial = match.group(1)
    elif stripped.endswith("{"):
        state = "object_open"
        partial = ""
    elif stripped.endswith(","):
        state = "next_key"
        partial = ""
    elif match := _VALUE_AFTER_COLON.search(left):
        state = "value"
        partial = match.group(1)
        current_key = _last_present_key(left)
    else:
        state = "object"
        partial = ""

    return CursorContext(
        buffer=buffer,
        position=position,
        state=state,
        partial=partial,
        present_keys=present,
        current_key=current_key,
        duplicate_keys=duplicates,
    )
