from __future__ import annotations

import argparse
import html
import json
from typing import Any

from .core import JsonlWorld, derive_cursor_context
from .finalize import finalize_selection
from .suggestion import suggest_keys


def _schema(value: str) -> dict[str, Any]:
    return json.loads(value)


def complete_payload(schema_json: str, rows_jsonl: str, buffer: str) -> list[dict[str, Any]]:
    world = JsonlWorld.from_jsonl_text(_schema(schema_json), rows_jsonl)
    context = derive_cursor_context(buffer)
    return [item.to_dict() for item in suggest_keys(world, context)]


def select_payload(schema_json: str, rows_jsonl: str, buffer: str, index: int) -> dict[str, Any]:
    world = JsonlWorld.from_jsonl_text(_schema(schema_json), rows_jsonl)
    context = derive_cursor_context(buffer)
    options = suggest_keys(world, context)
    return finalize_selection(options, index)


def build_preview_html(schema_json: str, rows_jsonl: str) -> str:
    cases = ["{", '{"ti', '{"kind":"task",']
    blocks = []
    for buffer in cases:
        payload = complete_payload(schema_json, rows_jsonl, buffer)
        rendered = html.escape(json.dumps(payload, ensure_ascii=False, indent=2))
        blocks.append(f"<section><h2>{html.escape(buffer)}</h2><pre>{rendered}</pre></section>")
    return "<!doctype html><meta charset='utf-8'><title>hq preview</title>" + "".join(blocks)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="hq")
    sub = parser.add_subparsers(dest="command", required=True)

    complete = sub.add_parser("complete")
    complete.add_argument("--schema-json", required=True)
    complete.add_argument("--rows-jsonl", default="")
    complete.add_argument("--buffer", required=True)

    select = sub.add_parser("select")
    select.add_argument("--schema-json", required=True)
    select.add_argument("--rows-jsonl", default="")
    select.add_argument("--buffer", required=True)
    select.add_argument("--index", type=int, required=True)

    preview = sub.add_parser("preview")
    preview.add_argument("--schema-json", required=True)
    preview.add_argument("--rows-jsonl", default="")

    args = parser.parse_args(argv)
    if args.command == "complete":
        print(json.dumps(complete_payload(args.schema_json, args.rows_jsonl, args.buffer), ensure_ascii=False, sort_keys=True))
    elif args.command == "select":
        print(json.dumps(select_payload(args.schema_json, args.rows_jsonl, args.buffer, args.index), ensure_ascii=False, sort_keys=True))
    elif args.command == "preview":
        print(build_preview_html(args.schema_json, args.rows_jsonl))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
