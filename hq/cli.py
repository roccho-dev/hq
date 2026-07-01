from __future__ import annotations

import json
import sys
from typing import Any, Iterable, Mapping, Sequence

from .finalize import append_instruction_jsonl
from .terminal_surface import autocomplete_payload, choose_payload

WIDTH = 72

SCHEMA: dict[str, Any] = {
    "type": "object",
    "required": ["status", "title"],
    "properties": {
        "status": {"type": "string", "enum": ["done", "draft", "deferred"], "description": "current work state"},
        "title": {"type": "string", "description": "short task title"},
        "assignee": {"type": "string", "description": "owner or reviewer"},
        "priority": {"type": "string", "description": "triage priority"},
        "due": {"type": "string", "description": "target date"},
        "ticket": {"type": "string", "description": "external issue id"},
        "tags": {"type": "array", "description": "list of labels"},
    },
}

ROWS_JSONL = '{"status":"done","title":"Add terminal preview"}\n'


def hr(width: int = WIDTH) -> str:
    return "+" + "-" * width + "+"


def row(text: str = "", width: int = WIDTH) -> str:
    return "| " + text[: width - 2].ljust(width - 2) + " |"


def block(title: str, lines: Iterable[str]) -> list[str]:
    return [title, "", hr(), *[row(line) for line in lines], hr()]


def _scenario(buffer_with_cursor: str) -> tuple[str, int]:
    position = buffer_with_cursor.index("|")
    return buffer_with_cursor.replace("|", "", 1), position


def _property_detail(key: str) -> str:
    prop = SCHEMA.get("properties", {}).get(key, {})
    detail = prop.get("description")
    return str(detail) if detail else "schema key"


def _json_lines(value: Mapping[str, Any]) -> list[str]:
    return json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True).splitlines()


def _apply_edit(buffer: str, position: int, edit: Mapping[str, Any]) -> str:
    kind = edit.get("kind")
    if kind in {"insert_key", "complete_key"}:
        partial = str(edit.get("replacePartial") or "")
        start = position - len(partial)
        return buffer[:start] + str(edit.get("text", "")) + buffer[position:]
    if kind == "set_value":
        partial = str(edit.get("replacePartial") or "")
        start = position - len(partial)
        return buffer[:start] + str(edit.get("text", "")) + buffer[position:]
    if kind == "rename_key":
        before = '"' + str(edit.get("from")) + '"'
        after = '"' + str(edit.get("to")) + '"'
        return buffer.replace(before, after, 1)
    return buffer


def _candidate_rows(payload: Mapping[str, Any]) -> list[str]:
    suggestions = list(payload.get("suggestions", []))
    if not suggestions:
        return ["completion", "  (no suggestions)"]
    lines = ["completion", " +----+----------------------+----------+------------------------------+"]
    for idx, item in enumerate(suggestions[:5]):
        mark = ">" if idx == 0 else " "
        label = str(item.get("label", ""))
        kind = str(item.get("detail", item.get("kind", "")))
        meaning = item.get("meaning", {})
        key = str(meaning.get("key", label)) if isinstance(meaning, Mapping) else label
        detail = _property_detail(key) if item.get("kind") == "key" else str(item.get("detail", ""))
        lines.append(f" | {mark:<2} | {label:<20} | {kind:<8} | {detail:<28} |")
    lines.append(" +----+----------------------+----------+------------------------------+")
    return lines


def _diagnostic_rows(payload: Mapping[str, Any]) -> list[str]:
    diagnostics = list(payload.get("diagnostics", []))
    if not diagnostics:
        return []
    lines = ["diagnostics", " +----+----------------+------------------------------------------+"]
    for item in diagnostics:
        lines.append(f" | !  | {str(item.get('code', '')):<14} | {str(item.get('message', '')):<40} |")
    lines.append(" +----+----------------+------------------------------------------+")
    fixes = [fix for item in diagnostics for fix in item.get("fixes", [])]
    if fixes:
        lines.extend(["", "quick fix", " +----+----------------------+------------------------------+"])
        for idx, fix in enumerate(fixes[:3]):
            mark = ">" if idx == 0 else " "
            lines.append(f" | {mark:<2} | {str(fix.get('label', '')):<20} | {str(fix.get('detail', '')):<28} |")
        lines.append(" +----+----------------------+------------------------------+")
    return lines


def _context_rows(payload: Mapping[str, Any]) -> list[str]:
    context = payload.get("context", {})
    if not isinstance(context, Mapping):
        return []
    return [
        "context",
        f"  state: {context.get('state', '')}",
        f"  partial: {context.get('partial', '')}",
        f"  present keys: {', '.join(context.get('presentKeys', [])) or '-'}",
        f"  missing required: {', '.join(context.get('missingRequired', [])) or '-'}",
    ]


def _preview_rows(buffer: str, position: int, payload: Mapping[str, Any]) -> list[str]:
    suggestions = list(payload.get("suggestions", []))
    selected = suggestions[0] if suggestions else None
    if selected is None:
        diagnostics = list(payload.get("diagnostics", []))
        fixes = [fix for item in diagnostics for fix in item.get("fixes", [])]
        selected = fixes[0] if fixes else None
    if selected is None:
        return []
    result = _apply_edit(buffer, position, selected.get("edit", {}))
    lines = [
        "preview",
        f"  selected: {selected.get('label', '')}",
        f"  result: {result[:58]}",
        "",
        "compile draft",
    ]
    for line in _json_lines(selected.get("compileDraft", {})):
        lines.append("  " + line)
    return lines


def view(title: str, buffer_with_cursor: str) -> list[str]:
    buffer, position = _scenario(buffer_with_cursor)
    payload = autocomplete_payload(SCHEMA, ROWS_JSONL, buffer, position)
    diagnostics = _diagnostic_rows(payload)
    lines = [
        "hq autocomplete",
        f"> {buffer_with_cursor}",
        "",
        *_context_rows(payload),
        "",
        *diagnostics,
        *([""] if diagnostics else []),
        *_candidate_rows(payload),
        "",
        *_preview_rows(buffer, position, payload),
        "",
        "Enter accept  Tab insert  Esc close  Ctrl-N/P move",
    ]
    return block(title, [line for line in lines if line != ""])


def accept_view() -> list[str]:
    buffer, _position = _scenario('{"status": "done", "ti|')
    instruction = choose_payload(SCHEMA, ROWS_JSONL, buffer, 0)
    jsonl = append_instruction_jsonl("", instruction).strip()
    lines = [
        "accepted suggestion",
        "  source: choose_payload(schema, rows, buffer, 0)",
        "",
        "final instruction",
        *_json_lines(instruction),
        "",
        "jsonl row",
        jsonl,
        "",
        "Enter queue  Esc cancel  E edit",
    ]
    return block("UI 07: accept confirmation, instruction ready", lines)


def demo_autocomplete_screen() -> str:
    screens = [
        view("UI 01: object open, key suggestions", "{|"),
        view("UI 02: key fragment", '{"st|'),
        view("UI 03: value fragment, enum suggestions", '{"status": "d|'),
        view("UI 04: next key after completed pair", '{"status": "done", "ti|'),
        view("UI 05: duplicate key blocked", '{"status": "done", "status|'),
        view("UI 06: unknown key, did-you-mean", '{"statuz": "done"|'),
        accept_view(),
        view("UI 08: full engine-backed surface", '{"status": "done", "title": "Add CI UX preview", |'),
    ]
    return "\n\n".join("\n".join(screen) for screen in screens) + "\n"


def main(argv: Sequence[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    command = argv[0] if argv else "demo-autocomplete"
    if command == "demo-autocomplete":
        print(demo_autocomplete_screen(), end="")
        return 0
    print("usage: python -m hq demo-autocomplete", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
