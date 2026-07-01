#!/usr/bin/env python3
from __future__ import annotations

import argparse
import struct
import zlib
from pathlib import Path


def _glyph(rows: str) -> tuple[str, ...]:
    return tuple(rows.splitlines())


FONT = {
    " ": _glyph("00000\n00000\n00000\n00000\n00000\n00000\n00000"),
    "?": _glyph("01110\n10001\n00001\n00010\n00100\n00000\n00100"),
    "!": _glyph("00100\n00100\n00100\n00100\n00100\n00000\n00100"),
    ":": _glyph("00000\n00100\n00100\n00000\n00100\n00100\n00000"),
    ".": _glyph("00000\n00000\n00000\n00000\n00000\n01100\n01100"),
    ",": _glyph("00000\n00000\n00000\n00000\n01100\n00100\n01000"),
    "-": _glyph("00000\n00000\n00000\n11111\n00000\n00000\n00000"),
    "_": _glyph("00000\n00000\n00000\n00000\n00000\n00000\n11111"),
    "=": _glyph("00000\n00000\n11111\n00000\n11111\n00000\n00000"),
    "[": _glyph("01110\n01000\n01000\n01000\n01000\n01000\n01110"),
    "]": _glyph("01110\n00010\n00010\n00010\n00010\n00010\n01110"),
    "{": _glyph("00010\n00100\n00100\n01000\n00100\n00100\n00010"),
    "}": _glyph("01000\n00100\n00100\n00010\n00100\n00100\n01000"),
    "(": _glyph("00010\n00100\n01000\n01000\n01000\n00100\n00010"),
    ")": _glyph("01000\n00100\n00010\n00010\n00010\n00100\n01000"),
    ">": _glyph("10000\n01000\n00100\n00010\n00100\n01000\n10000"),
    "<": _glyph("00001\n00010\n00100\n01000\n00100\n00010\n00001"),
    "^": _glyph("00100\n01010\n10001\n00000\n00000\n00000\n00000"),
    "\"": _glyph("01010\n01010\n01010\n00000\n00000\n00000\n00000"),
    "'": _glyph("00100\n00100\n01000\n00000\n00000\n00000\n00000"),
    "/": _glyph("00001\n00010\n00100\n01000\n10000\n00000\n00000"),
    "\\": _glyph("10000\n01000\n00100\n00010\n00001\n00000\n00000"),
    "0": _glyph("01110\n10001\n10011\n10101\n11001\n10001\n01110"),
    "1": _glyph("00100\n01100\n00100\n00100\n00100\n00100\n01110"),
    "2": _glyph("01110\n10001\n00001\n00010\n00100\n01000\n11111"),
    "3": _glyph("11110\n00001\n00001\n01110\n00001\n00001\n11110"),
    "4": _glyph("00010\n00110\n01010\n10010\n11111\n00010\n00010"),
    "5": _glyph("11111\n10000\n10000\n11110\n00001\n00001\n11110"),
    "6": _glyph("01110\n10000\n10000\n11110\n10001\n10001\n01110"),
    "7": _glyph("11111\n00001\n00010\n00100\n01000\n01000\n01000"),
    "8": _glyph("01110\n10001\n10001\n01110\n10001\n10001\n01110"),
    "9": _glyph("01110\n10001\n10001\n01111\n00001\n00001\n01110"),
    "A": _glyph("01110\n10001\n10001\n11111\n10001\n10001\n10001"),
    "B": _glyph("11110\n10001\n10001\n11110\n10001\n10001\n11110"),
    "C": _glyph("01110\n10001\n10000\n10000\n10000\n10001\n01110"),
    "D": _glyph("11110\n10001\n10001\n10001\n10001\n10001\n11110"),
    "E": _glyph("11111\n10000\n10000\n11110\n10000\n10000\n11111"),
    "F": _glyph("11111\n10000\n10000\n11110\n10000\n10000\n10000"),
    "G": _glyph("01110\n10001\n10000\n10111\n10001\n10001\n01110"),
    "H": _glyph("10001\n10001\n10001\n11111\n10001\n10001\n10001"),
    "I": _glyph("01110\n00100\n00100\n00100\n00100\n00100\n01110"),
    "J": _glyph("00111\n00010\n00010\n00010\n10010\n10010\n01100"),
    "K": _glyph("10001\n10010\n10100\n11000\n10100\n10010\n10001"),
    "L": _glyph("10000\n10000\n10000\n10000\n10000\n10000\n11111"),
    "M": _glyph("10001\n11011\n10101\n10101\n10001\n10001\n10001"),
    "N": _glyph("10001\n11001\n10101\n10011\n10001\n10001\n10001"),
    "O": _glyph("01110\n10001\n10001\n10001\n10001\n10001\n01110"),
    "P": _glyph("11110\n10001\n10001\n11110\n10000\n10000\n10000"),
    "Q": _glyph("01110\n10001\n10001\n10001\n10101\n10010\n01101"),
    "R": _glyph("11110\n10001\n10001\n11110\n10100\n10010\n10001"),
    "S": _glyph("01111\n10000\n10000\n01110\n00001\n00001\n11110"),
    "T": _glyph("11111\n00100\n00100\n00100\n00100\n00100\n00100"),
    "U": _glyph("10001\n10001\n10001\n10001\n10001\n10001\n01110"),
    "V": _glyph("10001\n10001\n10001\n10001\n10001\n01010\n00100"),
    "W": _glyph("10001\n10001\n10001\n10101\n10101\n10101\n01010"),
    "X": _glyph("10001\n10001\n01010\n00100\n01010\n10001\n10001"),
    "Y": _glyph("10001\n10001\n01010\n00100\n00100\n00100\n00100"),
    "Z": _glyph("11111\n00001\n00010\n00100\n01000\n10000\n11111"),
}


class Canvas:
    def __init__(self, width: int, height: int, color: tuple[int, int, int]):
        self.width = width
        self.height = height
        self.pixels = bytearray(width * height * 3)
        for y in range(height):
            for x in range(width):
                self.set_pixel(x, y, color)

    def set_pixel(self, x: int, y: int, color: tuple[int, int, int]) -> None:
        if not (0 <= x < self.width and 0 <= y < self.height):
            return
        offset = (y * self.width + x) * 3
        self.pixels[offset : offset + 3] = bytes(color)

    def rect(self, x: int, y: int, w: int, h: int, color: tuple[int, int, int]) -> None:
        for yy in range(y, y + h):
            for xx in range(x, x + w):
                self.set_pixel(xx, yy, color)

    def text(self, x: int, y: int, text: str, color: tuple[int, int, int], scale: int = 2) -> None:
        cursor = x
        for char in text.upper():
            glyph = FONT.get(char, FONT["?"])
            for gy, row in enumerate(glyph):
                for gx, bit in enumerate(row):
                    if bit == "1":
                        self.rect(cursor + gx * scale, y + gy * scale, scale, scale, color)
            cursor += 6 * scale


def _chunk(kind: bytes, data: bytes) -> bytes:
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)


def write_png(path: Path, canvas: Canvas) -> None:
    rows = []
    stride = canvas.width * 3
    for y in range(canvas.height):
        start = y * stride
        rows.append(b"\x00" + bytes(canvas.pixels[start : start + stride]))
    raw = b"".join(rows)
    data = b"\x89PNG\r\n\x1a\n"
    data += _chunk(b"IHDR", struct.pack(">IIBBBBB", canvas.width, canvas.height, 8, 2, 0, 0, 0))
    data += _chunk(b"IDAT", zlib.compress(raw, level=9))
    data += _chunk(b"IEND", b"")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)


def render_text(text: str, path: Path) -> None:
    lines = text.rstrip("\n").splitlines()
    if not lines:
        raise ValueError("input text is empty")
    scale = 2
    width = max(980, min(1800, max(len(line) for line in lines) * 6 * scale + 64))
    height = len(lines) * 18 + 52
    canvas = Canvas(width, height, (16, 18, 24))
    canvas.rect(0, 0, width, 34, (35, 42, 55))
    canvas.text(24, 12, "ACTUAL CLI OUTPUT: PYTHON -M HQ DEMO-AUTOCOMPLETE", (146, 232, 166), scale=2)
    y = 50
    for line in lines:
        stripped = line.strip().upper()
        color = (232, 236, 243)
        if stripped.startswith("CASE") or stripped.startswith("ACCEPT"):
            color = (146, 232, 166)
        elif stripped.startswith(">") or stripped.startswith("[") or stripped.startswith("  >"):
            color = (139, 213, 255)
        elif stripped.startswith("BUFFER") or stripped.startswith("CURSOR"):
            color = (255, 213, 139)
        canvas.text(24, y, line, color, scale=scale)
        y += 18
    write_png(path, canvas)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", required=True)
    parser.add_argument("--out", default="artifacts/hq-terminal-autocomplete-preview.png")
    args = parser.parse_args()
    render_text(Path(args.input).read_text(encoding="utf-8"), Path(args.out))


if __name__ == "__main__":
    main()
