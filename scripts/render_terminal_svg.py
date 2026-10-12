#!/usr/bin/env python3
"""Render plain terminal text into a dark-theme SVG screenshot (stdlib only)."""

from __future__ import annotations

import argparse
import html
import pathlib
import sys


def render(text: str, title: str, outfile: pathlib.Path, max_lines: int = 36) -> None:
    lines = text.replace("\r\n", "\n").replace("\r", "\n").split("\n")
    # Drop trailing empties
    while lines and lines[-1] == "":
        lines.pop()
    if len(lines) > max_lines:
        lines = lines[: max_lines - 1] + ["… (truncated for screenshot)"]

    char_w = 7.8
    line_h = 18
    pad_x = 28
    pad_y = 56
    footer = 28
    width = int(max(88, max((len(l) for l in lines), default=40)) * char_w + pad_x * 2)
    width = min(width, 980)
    height = pad_y + len(lines) * line_h + footer

    # Escape and soft-wrap visually by truncating long lines for SVG width
    max_chars = int((width - pad_x * 2) / char_w)
    esc_lines = []
    for l in lines:
        if len(l) > max_chars:
            l = l[: max_chars - 1] + "…"
        esc_lines.append(html.escape(l) if l else " ")

    tspans = []
    for i, l in enumerate(esc_lines):
        fill = "#E2E8F0"
        if l.startswith("$ "):
            fill = "#5EEAD4"
        elif "WARNING" in l or "[WARNING]" in l:
            fill = "#FBBF24"
        elif "CRITICAL" in l or "[CRITICAL]" in l:
            fill = "#F87171"
        elif l.startswith("  [") or "INFO" in l[:20]:
            fill = "#93C5FD"
        y = pad_y + i * line_h
        tspans.append(f'<tspan x="{pad_x}" y="{y}" fill="{fill}">{l}</tspan>')

    svg = f'''<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}" role="img" aria-label="{html.escape(title)}">
  <title>{html.escape(title)}</title>
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="#0B1220"/>
      <stop offset="100%" stop-color="#070B14"/>
    </linearGradient>
  </defs>
  <rect width="{width}" height="{height}" rx="12" fill="url(#bg)" stroke="#1E293B" stroke-width="2"/>
  <circle cx="28" cy="24" r="6" fill="#F87171"/>
  <circle cx="50" cy="24" r="6" fill="#FBBF24"/>
  <circle cx="72" cy="24" r="6" fill="#34D399"/>
  <text x="96" y="28" font-family="ui-sans-serif, system-ui, sans-serif" font-size="13" fill="#64748B">{html.escape(title)}</text>
  <text font-family="ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace" font-size="13" xml:space="preserve">
    {"".join(tspans)}
  </text>
  <text x="{pad_x}" y="{height - 10}" font-family="ui-sans-serif, system-ui, sans-serif" font-size="11" fill="#475569">Demo environment output · credentials redacted</text>
</svg>
'''
    outfile.write_text(svg, encoding="utf-8")
    print(f"wrote {outfile} ({width}x{height})")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("infile", type=pathlib.Path)
    ap.add_argument("outfile", type=pathlib.Path)
    ap.add_argument("--title", default="pgscope")
    ap.add_argument("--max-lines", type=int, default=36)
    args = ap.parse_args()
    text = args.infile.read_text(encoding="utf-8", errors="replace")
    # Redact obvious local paths / user home
    text = text.replace("/Users/haykghukasyan", "~")
    render(text, args.title, args.outfile, args.max_lines)
    return 0


if __name__ == "__main__":
    sys.exit(main())
