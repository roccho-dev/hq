from __future__ import annotations

import json
import sys
from typing import Any, Sequence

from .terminal_surface import choose_payload, complete_payload

SCHEMA: dict[str, Any] = {
    "type": "object",
    "required": ["kind", "title"],
    "properties": {
        "kind": {"type": "string"},
        "title": {"type": "string"},
        "status": {"type": "string"},
    },
}

ROWS_JSONL = '{"kind":"task","title":"ship preview"}\n'


def _completion_case(title: str, buffer: str) -> list[str]:
    payload = complete_payload(SCHEMA, ROWS_JSONL, buffer)
    lines = [
        f"CASE: {title}",
        f"BUFFER: {buffer}",
        "CURSOR: " + " " * len("BUFFER: ") + " " * len(buffer) + "^",
        "CANDIDATES:",
    ]
    for index, item in enumerate(payload):
        marker = ">" if index == 0 else " "
        draft = item["compileDraft"]
        edit = item["edit"]
        lines.append(f"  {marker} [{index}] {item['label']}")
        lines.append(f"      edit: insert {edit['text']} replacing {edit['replacePartial']!r}")
        lines.append(f"      draft: {draft['op']} {draft['key']}")
    if not payload:
        lines.append("  no candidates")
    return lines


def demo_autocomplete_screen() -> str:
    chosen = choose_payload(SCHEMA, ROWS_JSONL, "{", 0)
    final_row = json.dumps(chosen, ensure_ascii=False, sort_keys=True)
    lines = [
        "hq autocomplete demo",
        "actual command: python -m hq demo-autocomplete",
        "source: actual CLI stdout, rendered to CI PNG artifact",
        "",
    ]
    lines.extend(_completion_case("object open", "{"))
    lines.append("")
    lines.extend(_completion_case("partial key", '{"st'))
    lines.extend([
        "",
        "ACCEPT PREVIEW:",
        "  selected: [0] kind",
        "  final JSONL row:",
        f"  {final_row}",
    ])
    return "\n".join(lines) + "\n"


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
