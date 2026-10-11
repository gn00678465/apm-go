#!/usr/bin/env python3
"""Generator for cmd/apm-go/testdata/pack_metadata_oracle/*.json.

Runs the Oracle's `apm pack` in-process with a canned remote-metadata
outcome (the Oracle fetches over HTTP, so a real fetch cannot be a local
fixture) and records, for each invocation, the exit code, stdout, the files
on disk afterwards and how many times the fetch ran. Only the remote fetch
is replaced: the explicit and local outcomes, the warnings and the rendering
are the Oracle's own.
cmd/apm-go/pack_metadata_oracle_test.go replays every record against apm-go.

    COLUMNS=10000 NO_COLOR=1 uv run --project <oracle checkout> python \
        tools/pack_metadata_oracle_gen.py cmd/apm-go/testdata/pack_metadata_oracle

COLUMNS keeps Rich from wrapping lines. Regenerate against a newer Oracle
release when its pack output changes.
"""

import json
import os
import sys
import tempfile
from pathlib import Path

from click.testing import CliRunner

from apm_cli.commands.pack import pack_cmd
from apm_cli.marketplace.builder import MarketplaceBuilder, MetadataEnrichmentOutcome

APM_YML = """\
name: metadata-outcome
description: Metadata outcome regression fixture
version: 1.0.0
license: MIT
marketplace:
  owner:
    name: APM Tests
%s  packages:
    - name: remote-tool
      source: acme/remote-tool
      ref: 0123456789abcdef0123456789abcdef01234567
      category: Productivity
%s"""
TWO_OUTPUTS = "  outputs: [codex, claude]\n"
WITH_BUNDLE = "dependencies: {}\n"

calls = []

SHA = "0123456789abcdef0123456789abcdef01234567"


def project(packages, marketplace="", top=""):
    """An apm.yml with a marketplace: block holding *packages*."""
    return (
        "name: metadata-outcome\n"
        "description: Metadata outcome regression fixture\n"
        "version: 1.0.0\n"
        "license: MIT\n"
        "%smarketplace:\n"
        "  owner:\n"
        "    name: APM Tests\n"
        "%s  packages:\n%s"
    ) % (top, marketplace, packages)


def remote(name, extra=""):
    return (
        "    - name: %s\n"
        "      source: acme/%s\n"
        "      ref: %s\n"
        "      category: Productivity\n%s"
    ) % (name, name, SHA, extra)


def local(name, source):
    return "    - name: %s\n      source: %s\n      category: Productivity\n" % (name, source)


PACKAGE_APM_YML = "name: %s\ndescription: A local package\nversion: %s\n"
ROOT_WITHOUT_MARKETPLACE = "name: metadata-outcome\nversion: 1.0.0\nlicense: MIT\n"
LEGACY_MARKETPLACE_YML = (
    "name: legacy-marketplace\n"
    "description: Legacy marketplace\n"
    "version: 1.0.0\n"
    "owner:\n"
    "  name: APM Tests\n"
    # apm-go's default output path for a legacy file differs from the
    # Oracle's (marketplace.json at the root), so the fixture names it.
    "claude:\n"
    "  output: .claude-plugin/marketplace.json\n"
    "packages:\n%s"
)


def fetch_stub(outcomes):
    """Replace the remote fetch with *outcomes*: package name -> "status" or
    "failed:<cause>"; "*" is the default."""

    def stub(_self, pkg):
        calls.append(pkg.name)
        status, _, cause = outcomes.get(pkg.name, outcomes.get("*", "fetched")).partition(":")
        if status == "failed":
            return MetadataEnrichmentOutcome(pkg.name, "failed", cause=cause)
        if status == "empty":
            return MetadataEnrichmentOutcome(pkg.name, "empty")
        return MetadataEnrichmentOutcome(
            pkg.name, "fetched", values=(("description", "available"), ("version", "1.0.0"))
        )

    return stub


def record(out_dir, name, outcomes, argsets, outputs="", tail="", apm_yml=None, files=None):
    if isinstance(outcomes, str):
        outcomes = {"*": outcomes if outcomes != "failed" else "failed:request timed out"}
    MarketplaceBuilder._fetch_remote_metadata_outcome = fetch_stub(outcomes)
    if apm_yml is None:
        apm_yml = APM_YML % (outputs, tail)
    files = files or {}
    steps = []
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp).resolve()
        (root / "apm.yml").write_text(apm_yml, encoding="utf-8")
        for rel, content in files.items():
            (root / rel).parent.mkdir(parents=True, exist_ok=True)
            (root / rel).write_text(content, encoding="utf-8")
        os.chdir(root)
        for args in argsets:
            calls.clear()
            result = CliRunner().invoke(pack_cmd, args)
            steps.append(
                {
                    "args": args,
                    "exit": result.exit_code,
                    "stdout": result.stdout.replace(str(root), "<ROOT>"),
                    "files": sorted(
                        p.relative_to(root).as_posix() for p in root.rglob("*") if p.is_file()
                    ),
                    "fetch_calls": len(calls),
                }
            )
        os.chdir("/")
    doc = {"outcomes": outcomes, "apm_yml": apm_yml, "files": files, "steps": steps}
    (out_dir / (name + ".json")).write_text(json.dumps(doc, indent=1) + "\n", encoding="utf-8")


def main():
    out_dir = Path(sys.argv[1]).resolve()
    MarketplaceBuilder._ensure_auth = lambda _self: None
    record(out_dir, "failed-json", "failed", [["--json"]])
    record(out_dir, "failed-text", "failed", [[]])
    record(out_dir, "failed-strict-json", "failed", [["--strict-metadata", "--json"]])
    record(out_dir, "failed-strict-text", "failed", [["--strict-metadata"]])
    record(out_dir, "failed-strict-dry-run-json", "failed", [["--strict-metadata", "--dry-run", "--json"]])
    record(out_dir, "failed-strict-none", "failed", [["--strict-metadata", "-m", "none"]])
    record(out_dir, "failed-strict-codex", "failed", [["--strict-metadata", "-m", "codex"]], outputs=TWO_OUTPUTS)
    record(out_dir, "failed-strict-bundle", "failed", [["--strict-metadata"]], outputs=TWO_OUTPUTS, tail=WITH_BUNDLE)
    record(out_dir, "failed-strict-gates", "failed", [["--strict-metadata", "--check-clean", "--check-versions"]])
    record(out_dir, "failed-clean-text", "failed", [["--check-clean"]])
    record(out_dir, "failed-clean-dry-run-text", "failed", [["--check-clean", "--dry-run"]])
    record(out_dir, "failed-clean-json", "failed", [["--check-clean", "--json"]])
    record(out_dir, "failed-clean-none-json", "failed", [["--check-clean", "-m", "none", "--json"]])
    record(out_dir, "failed-clean-codex-json", "failed", [["--check-clean", "-m", "codex", "--json"]], outputs=TWO_OUTPUTS)
    record(out_dir, "failed-clean-two-outputs-text", "failed", [["--check-clean"]], outputs=TWO_OUTPUTS)
    record(
        out_dir,
        "fetched-build-then-clean",
        "fetched",
        [[], ["--check-clean"], ["--check-clean", "--json"], ["--check-clean", "--dry-run"]],
    )
    record(out_dir, "fetched-clean-missing-text", "fetched", [["--check-clean"]])
    record(out_dir, "fetched-clean-dry-run-json", "fetched", [["--check-clean", "--dry-run", "--json"]])
    record(out_dir, "fetched-clean-with-bundle-text", "fetched", [["--check-clean"]], tail=WITH_BUNDLE)
    record(out_dir, "fetched-clean-with-bundle-json", "fetched", [["--check-clean", "--json"]], tail=WITH_BUNDLE)
    record(out_dir, "fetched-json", "fetched", [["--json"]])
    record(out_dir, "fetched-codex-json", "fetched", [["-m", "codex", "--json"]], outputs=TWO_OUTPUTS)

    text_and_json = [[], ["--json"], ["--check-clean"], ["--check-clean", "--json"]]
    record(
        out_dir,
        "explicit",
        {},
        text_and_json,
        apm_yml=project(remote("remote-tool", "      description: Curated\n      version: 2.0.0\n")),
    )
    record(
        out_dir,
        "local",
        {},
        text_and_json,
        apm_yml=project(local("local-tool", "./pkgs/local-tool")),
        files={"pkgs/local-tool/apm.yml": PACKAGE_APM_YML % ("local-tool", "1.0.0")},
    )
    record(out_dir, "empty", "empty", text_and_json)

    two_failed = project(remote("first-tool") + remote("second-tool"))
    causes = {
        "first-tool": "failed:request timed out",
        "second-tool": "failed:authentication required",
    }
    record(
        out_dir,
        "two-failed",
        causes,
        [["--strict-metadata"], ["--strict-metadata", "--json"], [], ["--json"], ["--check-clean"]],
        apm_yml=two_failed,
    )

    lockstep = project(
        local("local-tool", "./pkgs/local-tool") + remote("remote-tool"),
        marketplace="  versioning:\n    strategy: lockstep\n",
    )
    record(
        out_dir,
        "failed-versions-and-clean",
        "failed",
        [["--check-versions", "--check-clean"], ["--check-versions", "--check-clean", "--json"]],
        apm_yml=lockstep,
        files={"pkgs/local-tool/apm.yml": PACKAGE_APM_YML % ("local-tool", "9.9.9")},
    )

    legacy = {"marketplace.yml": LEGACY_MARKETPLACE_YML % remote("remote-tool")}
    record(
        out_dir,
        "legacy-fetched",
        "fetched",
        [
            ["--json"],
            ["-m", "none", "--json"],
            [],
            ["--check-clean"],
            ["--check-clean", "--dry-run"],
            ["--check-clean", "--json"],
        ],
        apm_yml=ROOT_WITHOUT_MARKETPLACE,
        files=legacy,
    )
    record(
        out_dir,
        "legacy-failed",
        "failed",
        [["--check-clean", "--dry-run"], ["--check-clean"], ["--json"]],
        apm_yml=ROOT_WITHOUT_MARKETPLACE,
        files=legacy,
    )

    record(
        out_dir,
        "local-unparsable-metadata",
        {},
        [["--json"], []],
        apm_yml=project(local("pkg-a", "./pkgs/pkg-a")),
        files={"pkgs/pkg-a/apm.yml": "name: pkg-a\ndescription: [unclosed\n"},
    )
    record(
        out_dir,
        "local-outside-plugin-root",
        {},
        [["--json"], [], ["--check-clean"], ["--check-clean", "--dry-run"]],
        apm_yml=project(
            local("pkg-a", "./other/pkg-a"),
            marketplace="  metadata:\n    pluginRoot: ./plugins\n",
        ),
        files={"other/pkg-a/apm.yml": PACKAGE_APM_YML % ("pkg-a", "1.0.0")},
    )


if __name__ == "__main__":
    main()
