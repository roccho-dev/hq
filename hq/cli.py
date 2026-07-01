from __future__ import annotations

import sys
from typing import Iterable, Sequence

WIDTH = 60


def hr() -> str:
    return "+" + "-" * WIDTH + "+"


def row(text: str = "") -> str:
    return "| " + text[: WIDTH - 2].ljust(WIDTH - 2) + " |"


def block(title: str, lines: Iterable[str]) -> list[str]:
    return [title, "", hr(), *[row(line) for line in lines], hr()]


def candidate_table(widths: tuple[int, int, int], rows: Sequence[tuple[bool, str, str, str]]) -> list[str]:
    w1, w2, w3 = widths
    sep = " +----+" + "-" * (w1 + 2) + "+" + "-" * (w2 + 2) + "+" + "-" * (w3 + 2) + "+"
    out = ["completion", sep]
    for active, label, kind, detail in rows:
        mark = ">" if active else " "
        out.append(f" | {mark:<2} | {label:<{w1}} | {kind:<{w2}} | {detail:<{w3}} |")
    out.append(sep)
    return out


def ui_01() -> list[str]:
    lines = [
        "hq jsonl input",
        "> {",
        "    |",
        "",
        *candidate_table((10, 8, 25), [
            (True, "status", "required", "current work state"),
            (False, "title", "required", "short task title"),
            (False, "assignee", "optional", "owner or reviewer"),
            (False, "priority", "optional", "triage priority"),
            (False, "due", "optional", "target date"),
        ]),
        "",
        "preview",
        "  insert: \"status\":",
        "  result: { \"status\": | }",
        "",
        "compile draft",
        "  op: set_key",
        "  key: status",
        "  target: current_object",
        "",
        "Tab insert  Enter accept  Esc close  Ctrl-N/P move",
    ]
    return block("UI 01: object open, key suggestions", lines)


def ui_02() -> list[str]:
    lines = [
        "hq jsonl input",
        "> { \"st",
        "       |",
        "",
        *candidate_table((10, 8, 25), [
            (True, "status", "required", "current work state"),
            (False, "started_at", "optional", "timestamp when started"),
            (False, "story", "optional", "narrative / note"),
        ]),
        "",
        "preview",
        "  before: { \"st|",
        "  insert: atus\":",
        "  after:  { \"status\": |",
        "",
        "compile draft",
        "  op: complete_key",
        "  key: status",
        "  partial: st",
        "",
        "Tab insert  Enter accept  Esc close",
    ]
    return block("UI 02: key fragment", lines)


def ui_03() -> list[str]:
    lines = [
        "hq jsonl input",
        "> { \"status\": \"d",
        "              |",
        "",
        *candidate_table((8, 6, 29), [
            (True, "done", "enum", "task is complete"),
            (False, "draft", "enum", "task is not ready yet"),
            (False, "deferred", "enum", "task is postponed"),
        ]),
        "",
        "preview",
        "  before: { \"status\": \"d|",
        "  insert: one\"",
        "  after:  { \"status\": \"done\"|",
        "",
        "compile draft",
        "  op: set_value",
        "  key: status",
        "  value: done",
        "",
        "Enter accept  Tab insert  Esc close",
    ]
    return block("UI 03: value fragment, enum suggestions", lines)


def ui_04() -> list[str]:
    lines = [
        "hq jsonl input",
        "> { \"status\": \"done\", \"ti",
        "                      |",
        "",
        *candidate_table((7, 8, 28), [
            (True, "title", "required", "short task title"),
            (False, "ticket", "optional", "external issue id"),
            (False, "timeline", "optional", "progress note"),
        ]),
        "",
        "context",
        "  object: current",
        "  present keys: status",
        "  missing required: title",
        "",
        "preview",
        "  before: { \"status\": \"done\", \"ti|",
        "  insert: tle\":",
        "  after:  { \"status\": \"done\", \"title\": |",
        "",
        "compile draft",
        "  op: complete_key",
        "  key: title",
        "  reason: required_missing",
    ]
    return block("UI 04: next key after completed pair", lines)


def ui_05() -> list[str]:
    lines = [
        "hq jsonl input",
        "> { \"status\": \"done\", \"status",
        "                      |",
        "",
        "diagnostics",
        " +----+-------------+--------------------------------------+",
        " | !  | duplicate   | key already exists: status          |",
        " +----+-------------+--------------------------------------+",
        "",
        *candidate_table((8, 8, 27), [
            (True, "title", "required", "short task title"),
            (False, "assignee", "optional", "owner or reviewer"),
            (False, "priority", "optional", "triage priority"),
        ]),
        "",
        "preview",
        "  replace partial key: status -> title",
        "  result: { \"status\": \"done\", \"title\": |",
        "",
        "compile draft",
        "  op: replace_partial_key",
        "  from: status",
        "  to: title",
    ]
    return block("UI 05: duplicate key blocked", lines)


def ui_06() -> list[str]:
    lines = [
        "hq jsonl input",
        "> { \"statuz\": \"done\"",
        "     |",
        "",
        "diagnostics",
        " +----+-------------+--------------------------------------+",
        " | !  | unknown_key | statuz is not in schema             |",
        " +----+-------------+--------------------------------------+",
        "",
        "quick fix",
        " +----+-----------------------+----------------------------+",
        " | >  | rename to status     | closest schema key         |",
        " |    | keep as custom key   | mark as extension          |",
        " +----+-----------------------+----------------------------+",
        "",
        "preview",
        "  before: { \"statuz\": \"done\" }",
        "  after:  { \"status\": \"done\" }",
        "",
        "compile draft",
        "  op: rename_key",
        "  from: statuz",
        "  to: status",
    ]
    return block("UI 06: unknown key, did-you-mean", lines)


def ui_07() -> list[str]:
    lines = [
        "hq jsonl input",
        "> { \"status\": \"done\", \"title\": \"Add CI UX preview\" }",
        "",
        "accepted suggestion",
        " +--------------------------------------------------------+",
        " | key/value complete                                     |",
        " | status: done                                           |",
        " | title: Add CI UX preview                               |",
        " +--------------------------------------------------------+",
        "",
        "final instruction",
        " +--------------------------------------------------------+",
        " | {                                                      |",
        " |   \"op\": \"append_jsonl_instruction\",                    |",
        " |   \"payload\": {                                         |",
        " |     \"status\": \"done\",                                  |",
        " |     \"title\": \"Add CI UX preview\"                       |",
        " |   }                                                    |",
        " | }                                                      |",
        " +--------------------------------------------------------+",
        "",
        "Enter queue  Esc cancel  E edit",
    ]
    return block("UI 07: accept confirmation, instruction ready", lines)


def ui_08() -> list[str]:
    lines = [
        "hq complete",
        "> status=d|",
        "",
        "> done      enum   set status to done",
        "  draft     enum   set status to draft",
        "  deferred  enum   set status to deferred",
        "",
        "preview: status=done",
        "draft:   op=set_value key=status value=done",
        "",
        "Enter accept  Tab insert  Esc close",
    ]
    return block("UI 08: compact single-line mode", lines)


def ui_09() -> list[str]:
    lines = [
        "hq jsonl input",
        "> {",
        "    \"status\": \"done\",",
        "    \"title\": \"Add CI UX preview\",",
        "    \"pr\": |",
        "  }",
        "",
        *candidate_table((7, 6, 31), [
            (True, "5", "number", "hq terminal surface PR"),
            (False, "4", "number", "closed superseded PR"),
        ]),
        "",
        "context",
        "  key: pr",
        "  type: number",
        "  source: recent_prs",
        "",
        "preview",
        "  result: \"pr\": 5",
        "",
        "compile draft",
        "  op: set_value",
        "  key: pr",
        "  value: 5",
    ]
    return block("UI 09: multi-line object mode", lines)


def ui_10() -> list[str]:
    lines = [
        "hq autocomplete",
        "document: work-item.jsonl",
        "cursor: line 1 col 18",
        "mode: key",
        "",
        "buffer",
        "  { \"status\": \"done\", \"ti|",
        "",
        "suggestions",
        " +----+---------+----------+------------------------------+",
        " | >  | title   | required | short task title             |",
        " |    | ticket  | optional | external issue id            |",
        " |    | tags    | optional | list of labels               |",
        " +----+---------+----------+------------------------------+",
        "",
        "details",
        "  selected: title",
        "  reason: required_missing",
        "  edit: replace partial \"ti\" with \"title\":",
        "",
        "result",
        "  { \"status\": \"done\", \"title\": |",
        "",
        "compile draft",
        "  {",
        "    \"op\": \"complete_key\",",
        "    \"key\": \"title\",",
        "    \"target\": \"current_object\"",
        "  }",
        "",
        "Enter accept  Tab insert  Ctrl-Space details  Esc close",
    ]
    return block("UI 10: full LSP-like surface", lines)


def demo_autocomplete_screen() -> str:
    screens = [ui_01(), ui_02(), ui_03(), ui_04(), ui_05(), ui_06(), ui_07(), ui_08(), ui_09(), ui_10()]
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
