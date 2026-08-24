#!/usr/bin/env python3
"""One-shot generator for spec/conformance/depref-accept.json and
spec/conformance/python-repr.json (ticket 11 attempt 5).

Run against the PINNED Oracle checkout so the fixtures are the Oracle's own
observed behavior, not a re-derivation of what apm-go's port believes the
Oracle does:

    uv run --project /home/madao/projects/apm-mesh/apm python3 \
        tools/depref_conformance_gen.py \
        --out-accept spec/conformance/depref-accept.json \
        --out-repr spec/conformance/python-repr.json

The input lists below enumerate every accept/reject branch reachable from a
marketplace source coordinate that apm-go's manifest.ParseDepString and
internal/marketplace's pythonRepr* family are meant to reproduce: shorthand
(bare, ref, virtual path, host-qualified, host:port), https/http/ssh/SCP
URL forms, SSH users, ports, query/fragment stripping, percent-encoding,
control characters, empty/whitespace, IPv6/odd hosts, path traversal, and a
few GitLab/Azure-DevOps/Artifactory shapes that are Oracle-only grammar
apm-go's port deliberately does not implement (see AGENTS.md's "deliberate
but partial" parity philosophy) -- those rows are still generated and
recorded, tagged `known_gap` with a reason, precisely so the gap is a
documented finding rather than a silent one.

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
# Each row: (input, category, known_gap_reason_or_None)
DEPREF_INPUTS: list[tuple[str, str, str | None]] = [
    # -- shorthand: bare, ref, virtual path --------------------------------
    ("owner/repo", "shorthand", None),
    ("owner/repo#v1.0.0", "shorthand", None),
    ("owner/repo#^1.0.0", "shorthand", None),
    ("owner/repo.git", "shorthand", None),
    ("owner/REPO", "shorthand", None),
    ("a_b/c-d.e", "shorthand", None),
    ("1/2", "shorthand", None),
    ("owner/repo/sub/dir", "shorthand", None),
    ("owner/repo/prompts/review.prompt.md", "shorthand", None),
    ("owner/repo/skills/my-skill", "shorthand", None),
    ("just-one-word", "shorthand", None),
    ("owner/repo?x", "shorthand", None),
    ("pkg@mkt", "shorthand", None),
    ("owner/", "shorthand", None),
    ("owner//repo", "shorthand", None),
    # -- shorthand: host-qualified, host:port ------------------------------
    ("github.com/owner/repo", "shorthand-host", None),
    ("gitlab.com/owner/repo#main", "shorthand-host", None),
    ("gitlab.com/owner/repo/skills/my-skill", "shorthand-host", None),
    ("x.io/owner/repo", "shorthand-host", None),
    ("x/owner/repo", "shorthand-host", None),
    ("x/owner/repo/sub/dir", "shorthand-host", None),
    ("-x.io/owner/repo", "shorthand-host", None),
    ("x-.io/owner/repo", "shorthand-host", None),
    ("x..io/owner/repo", "shorthand-host", None),
    ("x.io:443/owner/repo", "shorthand-port", None),
    ("x.io:8443/owner/repo", "shorthand-port", None),
    ("x.io:70000/owner/repo", "shorthand-port", None),
    ("x.io:abc/owner/repo", "shorthand-port", None),
    ("owner:1234/repo", "shorthand-port", None),
    (":443/owner/repo", "shorthand-port", None),
    # -- https/http ----------------------------------------------------------
    ("https://gitlab.com/acme/repo.git", "https", None),
    ("https://gitlab.com/acme/repo.git#v2.0", "https", None),
    ("http://internal.example.com/team/project", "https", None),
    ("https://x/owner/repo", "https", None),
    ("https://x.io/owner/repo", "https", None),
    ("https://x.io/owner/repo?x", "https-query", None),
    ("https://x.io/owner/repo?x=1&y=2", "https-query", None),
    ("https://x.io/owner/repo?x#ref", "https-query", None),
    ("https://x.io/owner/repo#ref?notquery", "https-query", None),
    ("https://x.io:8443/owner/repo", "https-port", None),
    ("https://x.io:99999/owner/repo", "https-port", None),
    ("https://-x.io/owner/repo", "https", None),
    ("https://", "https", None),
    ("https:///owner/repo", "https", None),
    ("https://x.io/owner/repo.git", "https", None),
    ("https://x.io/owner/repo/sub/dir", "https", None),
    ("https://1.2.3.4/owner/repo", "https-ip", None),
    ("https://[::1]/owner/repo", "https-ipv6", None),
    # -- ssh:// ----------------------------------------------------------------
    ("ssh://git@host/owner/repo", "ssh", None),
    ("ssh://alice@host/owner/repo", "ssh-user", None),
    ("ssh://host/owner/repo", "ssh-user", None),
    ("ssh://git@host:7999/owner/repo.git", "ssh-port", None),
    ("ssh://alice@host/owner/repo?x", "ssh-query", None),
    ("ssh://alice@host/owner/repo#ref", "ssh", None),
    ("ssh://-alice@host/owner/repo", "ssh-user", None),
    ("ssh://%2DoProxyCommand=evil@host/owner/repo", "ssh-user", None),
    ("ssh://a.b+c_d@host/owner/repo", "ssh-user", None),
    # -- SCP shorthand --------------------------------------------------------
    ("git@github.com:owner/repo.git", "scp", None),
    ("git@github.com:owner/repo.git#v1.0.0", "scp", None),
    ("alice@host:owner/repo", "scp-user", None),
    ("-alice@host:owner/repo", "scp-user", None),
    ("git@gitlab.com:acme/repo/sub/path.git", "scp", None),
    # -- percent-encoding -------------------------------------------------------
    ("owner/%72epo", "percent", None),
    ("owner/%zzrepo", "percent", None),
    ("owner/%2e%2e/repo", "percent-traversal", None),
    ("owner/%00repo", "percent-control", None),
    # -- local paths -----------------------------------------------------------
    ("./packages/local", "local", None),
    ("./foo/bar", "local", None),
    (
        "../../../etc/passwd",
        "local-traversal",
        "The Oracle's is_local_path branch (reference.py:1754-1768) accepts "
        "ANY relative local path whose basename isn't '.'/'..' -- it "
        "performs NO traversal/escape check at all, so a '../'-climbing "
        "path is accepted as a local dependency (is_local=True). apm-go's "
        "containsEscape is a DELIBERATE, pre-existing security hardening "
        "beyond the Oracle (see depref.go's own doc comment) that rejects "
        "this outright; matching the Oracle here would be a security "
        "regression, not a parity fix, so this row's rejection is kept "
        "and NOT relaxed to match.",
    ),
    (
        "../secret",
        "local-traversal",
        "Same intentional divergence as '../../../etc/passwd' above.",
    ),
    (
        "%2e%2e/%2e%2e/etc/passwd",
        "local-traversal",
        "Same intentional divergence as '../../../etc/passwd' above: after "
        "percent-decoding it is the same is_local_path-accepted shape on "
        "the Oracle side.",
    ),
    ("~/my-skills", "local", None),
    ("/etc/passwd", "local-absolute", None),
    ("/absolute/path", "local-absolute", None),
    # -- control chars / empty / whitespace -------------------------------------
    ("", "empty", None),
    ("   ", "empty", None),
    ("owner/repo\x01", "control", None),
    ("owner/repo\t", "control", None),
    # -- GitLab / Azure DevOps / Artifactory (Oracle-only grammar) --------------
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
    (
        "dev.azure.com/org/project/repo",
        "ado",
        "Accepted on both sides, but apm-go has no ADO-specific "
        "org/project/repo segment-count handling -- see comment above.",
    ),
    (
        "myorg.visualstudio.com/project/repo",
        "ado-legacy",
        "Accepted on both sides, but apm-go has no Azure DevOps legacy "
        "*.visualstudio.com segment-count handling -- see comment above.",
    ),
    (
        "gitlab.com/group/subgroup/repo",
        "gitlab-nested",
        "Accepted on both sides, but apm-go's shorthand parser has no "
        "GitLab nested-group (>2 path segments) handling -- see comment "
        "above.",
    ),
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
]


def gen_depref_accept(oracle_src: str) -> list[dict]:
    sys.path.insert(0, oracle_src)
    from apm_cli.models.dependency.reference import DependencyReference

    rows = []
    for value, category, known_gap in DEPREF_INPUTS:
        row = {"input": value, "category": category}
        if known_gap:
            row["known_gap"] = known_gap
        try:
            ref = DependencyReference.parse(value)
            row["accepted"] = True
            row["is_local"] = bool(ref.is_local)
        except Exception as exc:  # noqa: BLE001 -- record any rejection reason
            row["accepted"] = False
            row["error_type"] = type(exc).__name__
        rows.append(row)
    return rows


def gen_python_repr(oracle_src: str) -> list[dict]:
    rows = []
    for literal in PYTHON_REPR_INPUTS:
        value = json.loads(literal)
        rows.append({"json": literal, "repr": repr(value)})
    return rows


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--oracle-src", default="src", help="Oracle checkout's src/ directory")
    ap.add_argument("--out-accept", required=True)
    ap.add_argument("--out-repr", required=True)
    args = ap.parse_args()

    accept_rows = gen_depref_accept(args.oracle_src)
    with open(args.out_accept, "w", encoding="utf-8") as f:
        json.dump(accept_rows, f, indent=2, ensure_ascii=True)
        f.write("\n")

    repr_rows = gen_python_repr(args.oracle_src)
    with open(args.out_repr, "w", encoding="utf-8") as f:
        json.dump(repr_rows, f, indent=2, ensure_ascii=True)
        f.write("\n")

    print(f"wrote {len(accept_rows)} depref rows -> {args.out_accept}")
    print(f"wrote {len(repr_rows)} repr rows -> {args.out_repr}")


if __name__ == "__main__":
    main()
