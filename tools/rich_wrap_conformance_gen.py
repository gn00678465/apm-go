#!/usr/bin/env python3
"""One-shot generator for spec/conformance/rich-wrap.json (ticket 14
attempt 2).

Run against the PINNED Oracle checkout's own installed `rich` package so
the fixture is Rich's own observed wrapping behavior, not a re-derivation
of what wrap.go's port believes Rich does:

    cd /home/madao/projects/apm-mesh/apm && .venv/bin/python3 \
        /home/madao/projects/apm-mesh/apm-go/tools/rich_wrap_conformance_gen.py \
        --oracle-commit c8d6cdec596e773a84b0839c33c28b6b0a217637 \
        --out /home/madao/projects/apm-mesh/apm-go/spec/conformance/rich-wrap.json

--oracle-commit MUST match tools/parity/oracle.pin, the same convention
tools/depref_conformance_gen.py already uses (AGENTS.md's "Schema sync
tests... depend on conformance spec files under spec/conformance/...
runtime inputs tracked in git, not generated").

Each row exercises Console.print's real wrap pipeline (rich.text.Text.wrap:
hard-newline split, rich._wrap.divide_line, Text.rstrip_end, Text.truncate)
end to end via an actual Console(width=..., file=StringIO()), NOT just
divide_line in isolation -- this is what internal/ux/wrap_test.go's
TestWrapOracleText_ConformsToRichFixture asserts wrap.go's wrapOracleText
against, row by row. The corner classes covered: plain short text (no wrap
needed), a message that needs ordinary multi-token word-wrap, a single
token longer than the width outright (chop_cells folding), wide CJK
ideographs (2-cell-width folding), and an embedded hard newline (per-line
offset reset) -- plus the two real apm-go marketplace messages themselves,
at both the Oracle's 80-cell fallback and a COLUMNS=100 override.

This script is NOT part of the Go build; it is a one-shot fixture
generator, run manually whenever the fixture needs regenerating.
"""

from __future__ import annotations

import argparse
import io
import json


def wrap_via_console(text: str, width: int) -> str:
    from rich.console import Console

    buf = io.StringIO()
    console = Console(
        file=buf,
        width=width,
        no_color=True,
        force_terminal=False,
        highlight=False,
        markup=False,
        soft_wrap=False,
        legacy_windows=False,
    )
    console.print(text, markup=False, highlight=False, end="")
    return buf.getvalue()


ROWS: list[dict] = [
    {
        "name": "short text needs no wrap",
        "text": "[x] short message",
        "width": 80,
    },
    {
        "name": "ordinary multi-token word-wrap",
        "text": (
            "[x] Failed to browse marketplace: Marketplace 'nonexistent' is not "
            "registered. Run 'apm-go marketplace add https://github.com/OWNER/REPO' "
            "or 'apm-go marketplace add OWNER/REPO' to register it, or 'apm-go "
            "marketplace list' to see registered marketplaces."
        ),
        "width": 80,
    },
    {
        "name": "list-empty info line",
        "text": (
            "[i] No marketplaces registered. Use 'apm-go marketplace add SOURCE' "
            "to register one (OWNER/REPO, HTTPS URL, SSH URL, or local path)."
        ),
        "width": 80,
    },
    {
        "name": "single token longer than width (ASCII fold)",
        "text": (
            "[x] Failed to browse marketplace: Marketplace '" + ("x" * 120) + "' "
            "is not registered. Run 'apm-go marketplace add "
            "https://github.com/OWNER/REPO' or 'apm-go marketplace add OWNER/REPO' "
            "to register it, or 'apm-go marketplace list' to see registered "
            "marketplaces."
        ),
        "width": 80,
    },
    {
        "name": "wide CJK ideographs fold at 2 cells each",
        "text": (
            "[x] Failed to browse marketplace: Marketplace '" + ("市場" * 20) + "' "
            "is not registered. Run 'apm-go marketplace add "
            "https://github.com/OWNER/REPO' or 'apm-go marketplace add OWNER/REPO' "
            "to register it, or 'apm-go marketplace list' to see registered "
            "marketplaces."
        ),
        "width": 80,
    },
    {
        "name": "embedded hard newline resets the line offset",
        "text": (
            "[x] Failed to browse marketplace: Marketplace 'one\ntwo' is not "
            "registered. Run 'apm-go marketplace add "
            "https://github.com/OWNER/REPO' or 'apm-go marketplace add OWNER/REPO' "
            "to register it, or 'apm-go marketplace list' to see registered "
            "marketplaces."
        ),
        "width": 80,
    },
    {
        "name": "COLUMNS=100 widens the wrap",
        "text": (
            "[x] Failed to browse marketplace: Marketplace 'nonexistent' is not "
            "registered. Run 'apm-go marketplace add https://github.com/OWNER/REPO' "
            "or 'apm-go marketplace add OWNER/REPO' to register it, or 'apm-go "
            "marketplace list' to see registered marketplaces."
        ),
        "width": 100,
    },
    {
        "name": "narrow width forces heavy wrapping",
        "text": "[!] Marketplace 'acme' has no plugins available to install right now",
        "width": 24,
    },
]


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--oracle-commit", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    rows = []
    for row in ROWS:
        wrapped = wrap_via_console(row["text"], row["width"])
        rows.append(
            {
                "name": row["name"],
                "text": row["text"],
                "width": row["width"],
                "wrapped": wrapped,
            }
        )

    doc = {"oracle_commit": args.oracle_commit, "rows": rows}
    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, ensure_ascii=False)
        f.write("\n")


if __name__ == "__main__":
    main()
