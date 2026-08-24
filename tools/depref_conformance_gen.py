#!/usr/bin/env python3
"""One-shot generator for spec/conformance/depref-accept.json and
spec/conformance/python-repr.json (ticket 11 attempts 5-6).

Run against the PINNED Oracle checkout so the fixtures are the Oracle's own
observed behavior, not a re-derivation of what apm-go's port believes the
Oracle does:

    uv run --project /home/madao/projects/apm-mesh/apm python3 \
        tools/depref_conformance_gen.py \
        --oracle-src /home/madao/projects/apm-mesh/apm/src \
        --oracle-commit c8d6cdec596e773a84b0839c33c28b6b0a217637 \
        --out-accept spec/conformance/depref-accept.json \
        --out-repr spec/conformance/python-repr.json

--oracle-commit MUST match tools/parity/oracle.pin (the parity suite's own
pinned commit) -- the conformance tests assert this directly (ticket 11
attempt 6's "Standards" fix: earlier fixtures had no embedded commit at
all, so a regeneration against a drifted Oracle checkout could not be
told apart from one against the real pin).

The input lists below enumerate every accept/reject branch reachable from a
marketplace source coordinate that apm-go's manifest.ParseDepString and
internal/marketplace's pythonRepr* family are meant to reproduce: shorthand
(bare, ref, virtual path, host-qualified, host:port), https/http/ssh/SCP
URL forms (including mixed-case schemes -- ticket 11 attempt 6's
reproducer 1), SSH users, ports, query/fragment stripping,
percent-encoding, control characters, empty/whitespace, IPv6/odd hosts,
path traversal, and a few GitLab/Azure-DevOps/Artifactory shapes that are
Oracle-only grammar apm-go's port deliberately does not implement (see
AGENTS.md's "deliberate but partial" parity philosophy) -- those rows are
still generated and recorded, tagged `known_gap` with a reason, precisely
so the gap is a documented finding rather than a silent one. Each
known_gap row ALSO carries apmgo_accepted/apmgo_is_local: the CURRENT,
deliberately-diverging apm-go behavior, verified by hand against the
implementation and hard-coded here (this script only ever calls the
Oracle, never apm-go) -- ticket 11 attempt 6's "lock both directions" fix:
the conformance test asserts these too, so an accidental future change to
apm-go's own behavior on a known-gap row -- either narrowing the gap or
widening it further -- shows up as a test failure, not a silent drift.

This script is NOT part of the Go build; it is a one-shot fixture
generator, run manually whenever the fixtures need regenerating (a new
Oracle pin, a newly-discovered case). Its own output is what the
conformance tests in internal/manifest and internal/marketplace consume.
"""

from __future__ import annotations

import argparse
import json
import sys


# ---------------------------------------------------------------------------
# depref-accept.json inputs
# ---------------------------------------------------------------------------
# Each row is a dict:
#   input:          the dependency string to feed DependencyReference.parse
#   category:       a grouping label (informational)
#   known_gap:      omitted, or a string explaining a DELIBERATE divergence
#   apmgo_accepted: only meaningful (and required) when known_gap is set --
#                   the current, documented apm-go accept/reject verdict
#   apmgo_is_local: only meaningful when known_gap is set AND apmgo_accepted
#                   is True
DEPREF_INPUTS: list[dict] = [
    # -- shorthand: bare, ref, virtual path --------------------------------
    {"input": "owner/repo", "category": "shorthand"},
    {"input": "owner/repo#v1.0.0", "category": "shorthand"},
    {"input": "owner/repo#^1.0.0", "category": "shorthand"},
    {"input": "owner/repo.git", "category": "shorthand"},
    {"input": "owner/REPO", "category": "shorthand"},
    {"input": "a_b/c-d.e", "category": "shorthand"},
    {"input": "1/2", "category": "shorthand"},
    {"input": "owner/repo/sub/dir", "category": "shorthand"},
    {"input": "owner/repo/prompts/review.prompt.md", "category": "shorthand"},
    {"input": "owner/repo/skills/my-skill", "category": "shorthand"},
    {"input": "just-one-word", "category": "shorthand"},
    {"input": "owner/repo?x", "category": "shorthand"},
    {"input": "pkg@mkt", "category": "shorthand"},
    {"input": "owner/", "category": "shorthand"},
    {"input": "owner//repo", "category": "shorthand"},
    # -- shorthand: host-qualified, host:port ------------------------------
    {"input": "github.com/owner/repo", "category": "shorthand-host"},
    {"input": "gitlab.com/owner/repo#main", "category": "shorthand-host"},
    {"input": "gitlab.com/owner/repo/skills/my-skill", "category": "shorthand-host"},
    {"input": "x.io/owner/repo", "category": "shorthand-host"},
    {"input": "x/owner/repo", "category": "shorthand-host"},
    {"input": "x/owner/repo/sub/dir", "category": "shorthand-host"},
    {"input": "-x.io/owner/repo", "category": "shorthand-host"},
    {"input": "x-.io/owner/repo", "category": "shorthand-host"},
    {"input": "x..io/owner/repo", "category": "shorthand-host"},
    {"input": "x.io:443/owner/repo", "category": "shorthand-port"},
    {"input": "x.io:8443/owner/repo", "category": "shorthand-port"},
    {"input": "x.io:70000/owner/repo", "category": "shorthand-port"},
    {"input": "x.io:abc/owner/repo", "category": "shorthand-port"},
    {"input": "owner:1234/repo", "category": "shorthand-port"},
    {"input": ":443/owner/repo", "category": "shorthand-port"},
    # -- https/http ---------------------------------------------------------
    {"input": "https://gitlab.com/acme/repo.git", "category": "https"},
    {"input": "https://gitlab.com/acme/repo.git#v2.0", "category": "https"},
    {"input": "http://internal.example.com/team/project", "category": "https"},
    {"input": "https://x/owner/repo", "category": "https"},
    {"input": "https://x.io/owner/repo", "category": "https"},
    {"input": "https://x.io/owner/repo?x", "category": "https-query"},
    {"input": "https://x.io/owner/repo?x=1&y=2", "category": "https-query"},
    {"input": "https://x.io/owner/repo?x#ref", "category": "https-query"},
    {"input": "https://x.io/owner/repo#ref?notquery", "category": "https-query"},
    {"input": "https://x.io:8443/owner/repo", "category": "https-port"},
    {"input": "https://x.io:99999/owner/repo", "category": "https-port"},
    {"input": "https://-x.io/owner/repo", "category": "https"},
    {"input": "https://", "category": "https"},
    {"input": "https:///owner/repo", "category": "https"},
    {"input": "https://x.io/owner/repo.git", "category": "https"},
    {"input": "https://x.io/owner/repo/sub/dir", "category": "https"},
    {"input": "https://1.2.3.4/owner/repo", "category": "https-ip"},
    {"input": "https://[::1]/owner/repo", "category": "https-ipv6"},
    # -- https/http/ssh: mixed-case schemes (ticket 11 attempt 6 reproducer 1) --
    {"input": "HTTPS://x.io/owner/repo", "category": "scheme-case"},
    {"input": "Https://x.io/owner/repo", "category": "scheme-case"},
    {"input": "hTTps://x.io/owner/repo", "category": "scheme-case"},
    {"input": "HTTP://x.io/owner/repo", "category": "scheme-case"},
    {"input": "Http://x.io/owner/repo", "category": "scheme-case"},
    {
        "input": "SSH://git@host.io/owner/repo",
        "category": "scheme-case",
    },
    {
        "input": "Ssh://git@host.io/owner/repo",
        "category": "scheme-case",
    },
    # -- ssh:// ---------------------------------------------------------------
    {"input": "ssh://git@host/owner/repo", "category": "ssh"},
    {"input": "ssh://alice@host/owner/repo", "category": "ssh-user"},
    {"input": "ssh://host/owner/repo", "category": "ssh-user"},
    {"input": "ssh://git@host:7999/owner/repo.git", "category": "ssh-port"},
    {"input": "ssh://alice@host/owner/repo?x", "category": "ssh-query"},
    {"input": "ssh://alice@host/owner/repo#ref", "category": "ssh"},
    {"input": "ssh://-alice@host/owner/repo", "category": "ssh-user"},
    {"input": "ssh://%2DoProxyCommand=evil@host/owner/repo", "category": "ssh-user"},
    {"input": "ssh://a.b+c_d@host/owner/repo", "category": "ssh-user"},
    # -- SCP shorthand ----------------------------------------------------------
    {"input": "git@github.com:owner/repo.git", "category": "scp"},
    {"input": "git@github.com:owner/repo.git#v1.0.0", "category": "scp"},
    {"input": "alice@host:owner/repo", "category": "scp-user"},
    {"input": "-alice@host:owner/repo", "category": "scp-user"},
    {"input": "git@gitlab.com:acme/repo/sub/path.git", "category": "scp"},
    # -- percent-encoding ---------------------------------------------------------
    {"input": "owner/%72epo", "category": "percent"},
    {"input": "owner/%zzrepo", "category": "percent"},
    {"input": "owner/%2e%2e/repo", "category": "percent-traversal"},
    {"input": "owner/%00repo", "category": "percent-control"},
    # -- local paths ----------------------------------------------------------
    {"input": "./packages/local", "category": "local"},
    {"input": "./foo/bar", "category": "local"},
    {
        "input": "../../../etc/passwd",
        "category": "local-traversal",
        "known_gap": (
            "The Oracle's is_local_path branch (reference.py:1754-1768) accepts "
            "ANY relative local path whose basename isn't '.'/'..' -- it "
            "performs NO traversal/escape check at all, so a '../'-climbing "
            "path is accepted as a local dependency (is_local=True). apm-go's "
            "containsEscape is a DELIBERATE, pre-existing security hardening "
            "beyond the Oracle (see depref.go's own doc comment) that rejects "
            "this outright; matching the Oracle here would be a security "
            "regression, not a parity fix, so this row's rejection is kept "
            "and NOT relaxed to match."
        ),
        "apmgo_accepted": False,
    },
    {
        "input": "../secret",
        "category": "local-traversal",
        "known_gap": "Same intentional divergence as '../../../etc/passwd' above.",
        "apmgo_accepted": False,
    },
    {
        "input": "%2e%2e/%2e%2e/etc/passwd",
        "category": "local-traversal",
        "known_gap": (
            "Same intentional divergence as '../../../etc/passwd' above: after "
            "percent-decoding it is the same is_local_path-accepted shape on "
            "the Oracle side."
        ),
        "apmgo_accepted": False,
    },
    {"input": "~/my-skills", "category": "local"},
    {"input": "/etc/passwd", "category": "local-absolute"},
    {"input": "/absolute/path", "category": "local-absolute"},
    # -- control chars / empty / whitespace ---------------------------------------
    {"input": "", "category": "empty"},
    {"input": "   ", "category": "empty"},
    {"input": "owner/repo\x01", "category": "control"},
    {"input": "owner/repo\t", "category": "control"},
    # -- GitLab / Azure DevOps / Artifactory (Oracle-only grammar) ----------------
    # These three all happen to be ACCEPTED by both sides (apm-go's generic
    # 3-segment host-qualified shorthand branch parses SOME dependency
    # reference out of them), so the accepted/is_local booleans this
    # fixture actually asserts already match -- but the STRUCTURED FIELDS
    # diverge (apm-go has no ADO org/project/repo or GitLab nested-group
    # segment-count logic, so owner/repo/virtual_path get assigned
    # differently than the Oracle's own semantics). Recorded as known_gap
    # to document that narrower, deeper divergence honestly rather than
    # implying full semantic parity this ticket does not claim
    # (deliberately bounded, AGENTS.md "deliberate but partial";
    # isValidRemoteCoordinate's own doc comment already named this corner).
    {
        "input": "dev.azure.com/org/project/repo",
        "category": "ado",
        "known_gap": (
            "Accepted on both sides, but apm-go has no ADO-specific "
            "org/project/repo segment-count handling -- see comment above."
        ),
        "apmgo_accepted": True,
        "apmgo_is_local": False,
    },
    {
        "input": "myorg.visualstudio.com/project/repo",
        "category": "ado-legacy",
        "known_gap": (
            "Accepted on both sides, but apm-go has no Azure DevOps legacy "
            "*.visualstudio.com segment-count handling -- see comment above."
        ),
        "apmgo_accepted": True,
        "apmgo_is_local": False,
    },
    {
        "input": "gitlab.com/group/subgroup/repo",
        "category": "gitlab-nested",
        "known_gap": (
            "Accepted on both sides, but apm-go's shorthand parser has no "
            "GitLab nested-group (>2 path segments) handling -- see comment "
            "above."
        ),
        "apmgo_accepted": True,
        "apmgo_is_local": False,
    },
    # --- attempt 7 (eval-ticket-11 Attempt 6 ruling): urlsplit netloc/path
    # semantics the hand-split URL parser missed. Each class gets both the
    # evaluator's reproducer and its nearby corners so the table locks the
    # CLASS, not the instance.
    {"input": "https://user:pass@x.io/owner/repo", "category": "url-userinfo"},
    {"input": "https://user@x.io/owner/repo", "category": "url-userinfo"},
    {"input": "ssh://%2Duser@host.io/owner/repo", "category": "ssh-userinfo-percent"},
    {"input": "https://x.io//owner/repo", "category": "url-leading-double-slash"},
    {"input": "https://x.io/owner//repo", "category": "url-internal-double-slash"},
    {"input": "https://x.io/owner/repo/", "category": "url-trailing-slash"},
    {"input": "ssh://host.io//owner/repo", "category": "ssh-leading-double-slash"},
    {"input": "ssh://host.io/owner//repo", "category": "ssh-internal-double-slash"},
    {"input": "ssh://host.io/owner/repo/", "category": "ssh-trailing-slash"},
    {"input": "https://x.io:0/owner/repo", "category": "url-port-zero"},
    {"input": "https://x.io:/owner/repo", "category": "url-port-empty"},
    {"input": "https://x.io:65536/owner/repo", "category": "url-port-overflow"},
    {"input": "ssh://host.io:0/owner/repo", "category": "ssh-port-zero"},
    {"input": "x.io:0/owner/repo", "category": "shorthand-port-zero"},
    {"input": "https://X.IO/owner/repo", "category": "url-uppercase-host"},
    {"input": "https://x.io/owner/%2572epo", "category": "url-double-encoded"},
    {"input": "owner/repo#%e0%a0", "category": "percent-truncated-utf8-ref"},
    {"input": "owner/%e0%a0repo", "category": "percent-truncated-utf8-repo"},
    {"input": "owner/%a0repo", "category": "percent-lone-continuation"},
    {"input": "owner/%f0%90repo", "category": "percent-truncated-4byte"},
    # --- attempt 8 (eval-ticket-11 Attempt 7 ruling): SSH userinfo must come
    # from ONE urlsplit-equivalent netloc split (last-'@' boundary, username
    # up to the first ':', empty username -> default "git").
    {"input": "ssh://one@two@host.io/owner/repo", "category": "ssh-userinfo-double-at"},
    {"input": "ssh://one@@host.io/owner/repo", "category": "ssh-userinfo-double-at"},
    {"input": "ssh://alice:pw@host.io/owner/repo", "category": "ssh-userinfo-password"},
    {"input": "ssh://alice:@host.io/owner/repo", "category": "ssh-userinfo-password"},
    {"input": "ssh://@host.io/owner/repo", "category": "ssh-userinfo-empty"},
    {"input": "ssh://:pw@host.io/owner/repo", "category": "ssh-userinfo-empty"},
]


# ---------------------------------------------------------------------------
# python-repr.json inputs (raw JSON literal text -> repr(json.loads(text)))
# ---------------------------------------------------------------------------
PYTHON_REPR_INPUTS: list[str] = [
    # scalars
    "null",
    "true",
    "false",
    '""',
    '"hello"',
    "42",
    "-7",
    "0",
    "-0",
    "1.0",
    "-0.0",
    "1.5",
    "-0.5",
    "3.14159",
    "1e20",
    "1e-5",
    "1e400",
    "-1e400",
    "123456789012345678901234567890",
    # strings: quoting / escaping
    '"it\'s"',
    '"say \\"hi\\""',
    '"both \' and \\""',
    '"tab\\there"',
    '"line\\nbreak"',
    '"cr\\rreturn"',
    '"back\\\\slash"',
    '"\\u0007"',
    '"\\u001b[31m"',
    '"h\\u00e9llo"',
    '"\\ud800"',
    '"\\udc00"',
    '"\\ud83d\\ude00"',
    '"\\u200b"',
    # lists / dicts
    "[1, 2, 3]",
    '[1, "a", null, true, false]',
    "[]",
    "{}",
    '{"x": 1}',
    '{"b": 1, "a": 2}',
    '{"a": 1, "b": 2, "a": 3}',
    '{"a": [1, 2], "b": {"c": 3}}',
    '[{"x": 1}, {"y": 2}]',
    # dict KEYS carrying a lone surrogate, and nested (ticket 11 attempt 6
    # reproducer 2: jsonScanner.parseObject used to convert a pyStr key to a
    # plain Go string, destroying the surrogate before repr ever saw it).
    '{"\\ud800": 1}',
    '{"a": {"\\ud800": 1}}',
    '{"\\ud800": "\\udc00"}',
    '[{"\\ud800": 1}, "\\udc00"]',
]


# ---------------------------------------------------------------------------
# whitespace_sweep: every code point checked against Python's str.isspace()
# (ticket 11 attempt 6 reproducer 3: pyStrTrimSpace used Go's
# unicode.IsSpace, which disagrees with Python's isspace() for U+001C-U+001F).
# Covers the full ASCII + Latin-1 Supplement range (every C0/C1 control
# character and every Latin-1 code point) plus the remaining Unicode
# Zs/Zl/Zp whitespace code points beyond Latin-1, plus a few explicit
# NON-whitespace confusables (ZWSP, the Mongolian vowel separator, BOM) so
# the sweep also catches over-matching, not just under-matching.
# ---------------------------------------------------------------------------
def whitespace_sweep_codepoints() -> list[int]:
    codepoints = list(range(0x00, 0x100))
    codepoints += [
        0x1680,
        0x2000, 0x2001, 0x2002, 0x2003, 0x2004,
        0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200A,
        0x2028, 0x2029, 0x202F, 0x205F, 0x3000,
        0x200B,  # zero-width space -- NOT whitespace
        0x180E,  # Mongolian vowel separator -- NOT whitespace (reclassified)
        0xFEFF,  # BOM / zero-width no-break space -- NOT whitespace
    ]
    return codepoints


def gen_depref_accept(oracle_src: str, oracle_commit: str) -> dict:
    sys.path.insert(0, oracle_src)
    from apm_cli.models.dependency.reference import DependencyReference

    rows = []
    for entry in DEPREF_INPUTS:
        value = entry["input"]
        row = {"input": value, "category": entry["category"]}
        known_gap = entry.get("known_gap")
        if known_gap:
            row["known_gap"] = known_gap
            row["apmgo_accepted"] = entry["apmgo_accepted"]
            if entry["apmgo_accepted"]:
                row["apmgo_is_local"] = entry["apmgo_is_local"]
        try:
            ref = DependencyReference.parse(value)
            row["accepted"] = True
            row["is_local"] = bool(ref.is_local)
        except Exception as exc:  # noqa: BLE001 -- record any rejection reason
            row["accepted"] = False
            row["error_type"] = type(exc).__name__
        rows.append(row)
    return {"oracle_commit": oracle_commit, "rows": rows}


def gen_python_repr(oracle_commit: str) -> dict:
    rows = []
    for literal in PYTHON_REPR_INPUTS:
        value = json.loads(literal)
        rows.append({"json": literal, "repr": repr(value)})
    whitespace_sweep = [
        {"codepoint": cp, "is_space": chr(cp).isspace()} for cp in whitespace_sweep_codepoints()
    ]
    return {"oracle_commit": oracle_commit, "rows": rows, "whitespace_sweep": whitespace_sweep}


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--oracle-src", default="src", help="Oracle checkout's src/ directory")
    ap.add_argument(
        "--oracle-commit",
        required=True,
        help="Oracle commit SHA this run's checkout is at -- must match tools/parity/oracle.pin",
    )
    ap.add_argument("--out-accept", required=True)
    ap.add_argument("--out-repr", required=True)
    args = ap.parse_args()

    accept_doc = gen_depref_accept(args.oracle_src, args.oracle_commit)
    with open(args.out_accept, "w", encoding="utf-8") as f:
        json.dump(accept_doc, f, indent=2, ensure_ascii=True)
        f.write("\n")

    repr_doc = gen_python_repr(args.oracle_commit)
    with open(args.out_repr, "w", encoding="utf-8") as f:
        json.dump(repr_doc, f, indent=2, ensure_ascii=True)
        f.write("\n")

    print(f"wrote {len(accept_doc['rows'])} depref rows -> {args.out_accept}")
    print(
        f"wrote {len(repr_doc['rows'])} repr rows + "
        f"{len(repr_doc['whitespace_sweep'])} whitespace_sweep rows -> {args.out_repr}"
    )


if __name__ == "__main__":
    main()
