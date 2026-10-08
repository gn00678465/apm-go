# Ticket 37 — realexec output contract for `marketplace package update`

Status: RULED (2026-10-08)

## What

`apm-go marketplace package update [NAME...]` (issue #25) writes the upgrade
`marketplace outdated` reports for a SHA-pinned package back to the authoring
config. The pinned Oracle (`b75a02b1`) has no counterpart: its
`marketplace package` group has `add`, `set` and `remove`, and `set` rejects
`--version` together with `--ref`. The parity runner compares apm-go against
Oracle output, so no corpus case can exist for this command; a waiver cannot
apply (nothing to waive against) and a pending case would misstate the
situation.

## Ruling

Owner ruling, 2026-10-08, given with `board continue --to-orchestrate`
(issue #25, step 3), quoted verbatim:

> Owner rulings for this card. (1) Stop rule 2: the owner APPROVES the apm-go-only command `marketplace package update` with the flags `--dry-run` and `--include-prerelease`. Pin its contract in tools/gate/realexec.sh, record the ruling in a ticket as PRODUCT.md requires, and update PRODUCT.md and ARCHITECTURE.md. (2) Stop rule 1: the owner ACCEPTS the HIGH impact on `marketplacePackageCmd`, limited to the one AddCommand line that registers the subcommand. Any other HIGH or CRITICAL symbol that must be MODIFIED (not only called) still needs a new ruling. (3) A major version change needs NO extra flag: upgrade it like any other version, same rule as `outdated`. (4) For an annotated tag write the peeled commit SHA (the `^{}` line) to `ref`, as the issue text requires.

This exception applies only to `marketplace package update`. It is the
mechanism ticket 34 recorded for `plugin validate`, granted again by name; it
does not extend by analogy to any other command, and every command the Oracle
does have keeps the waiver / pending-case rule unchanged.

## Contract

- Selection: every package, or the `NAME...` given (matched without case, as
  `package set` does). An unknown name: `package "<name>" not found`, exit 2,
  nothing written, no remote call.
- Verdict: the one `marketplace outdated` computes (`outdatedForPackage`).
  - Lowercase 40-hex `ref` with a display `version`, upgradable: `version`
    becomes the highest candidate's version (a leading `v` is kept), `ref` the
    commit of that tag (the `refs/tags/<tag>^{}` entry for an annotated tag).
  - Lowercase 40-hex `ref` with no `version`, upgradable: `ref` becomes the
    full SHA of the default-branch tip; no `version` is added.
  - Local package, named ref, SHA with a range, range with no `ref`, already
    up to date, `No matching tags found`: skipped.
  - An `[x]` row (ls-remote error, `Remote advertised no HEAD`): the whole run
    fails, exit 2, nothing written.
  - A major version change needs no extra flag.
- Write: only the bytes of the `version` and `ref` scalar values change. All
  entries are edited in one in-memory copy, validated, compared with the plan,
  and written once (temp file, fsync, rename). An entry that cannot be edited
  in place is an error, never a redraw.
- File contract. The write is a rename, which replaces the whole file, so
  the result equals an edit in place only inside this closed list:
  - Kept: the config file's permission bits, owner and group; when one of
    them cannot be kept, nothing is written.
  - Refused, exit 2, nothing written, when at least one entry would be
    updated (with nothing to update the file is not checked):
    - a symbolic link:
      `cannot update <path> in place: it is a symbolic link; edit the file it points to by hand`
    - another non-regular file:
      `cannot update <path> in place: it is not a regular file`
    - a second hard link (not checked on Windows):
      `cannot update <path> in place: it has more than one hard link; edit it by hand`
    - a file the process cannot open for writing:
      `cannot update <path> in place: <the open error>`
    The command never follows a symbolic link to write its target.
  - Not kept and not checked: POSIX ACLs, extended attributes, SELinux and
    other security labels; timestamps (the mtime is new after a write); a
    change another process makes to the file between the command's read and
    the rename; an error of the write itself, such as a directory that
    cannot be written, which shows in a real run only.
- Flags: `--dry-run` (every step of a real run but the write: it prints the
  same values and reports the same resolution, refusal, in-place replacement
  and validation errors with the same exit code, and leaves the file alone;
  an error of the write itself shows in a real run only), `--include-prerelease`
  (as in `outdated`). No other flag. The command does not run `pack`.
- Output, on stdout through the `ux` printers, in config order:
  - ` + Updated package '<name>': version <old> -> <new>, ref <old12> -> <new12>`
  - ` + Updated package '<name>': ref <old12> -> <new12>`
  - `--dry-run`: the same text as ` i Would update package ...`
  - ` i Skipped package '<name>': <note>`, only for a package given as `NAME`
  - last line: ` i <n> package(s) updated`, ` i <n> package(s) would be updated`,
    or ` i All packages are up to date`
  - failure: one ` x cannot update: package '<name>': <note>` per package
- Exit codes: 0 on success (nothing to update and `--dry-run` included); 2 on
  an unknown name, a resolution failure, a write or validation failure, and a
  config schema error; config-load errors otherwise follow `outdated`.

## Verification strength

`tools/gate/realexec.sh` (`mkt_update` steps) compares the four surfaces
ticket 34 names: complete stdout against a recorded transcript, stderr
asserted empty, the exact exit code, and the working tree byte-identical
before and after.

The steps there cannot use the network, so they cover: a local-only config
(plain, `--dry-run`, and with the package named), an unknown `NAME`, and a
schema error whose stdout is also compared with `marketplace outdated`'s.
The path that resolves a remote and writes the file is not in realexec; it
is covered by `go test` with a canned `RefLister`
(`internal/marketplace/authoring/update_test.go`,
`cmd/apm-go/marketplace_package_update_test.go`). It has not been run against
a real remote.

## Acceptance criteria

- [x] `cmd/apm-go/marketplace_package_update.go` records the deviation at the
  command site: no Oracle counterpart, contract pinned by
  `tools/gate/realexec.sh`, this ticket.
- [x] `tools/gate/realexec.sh` has the network-free `marketplace package
  update` steps at the verification strength above.
- [x] `PRODUCT.md` and `ARCHITECTURE.md` name the command and this ticket.
- [ ] The PR description quotes this ruling.
- [ ] Existing parity corpus stays at 96 cases / 0 unwaived.
