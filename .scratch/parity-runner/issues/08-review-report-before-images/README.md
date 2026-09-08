# Before-images: `.review/eval-report-plugin-init-doctor*.md`

Ticket 08 eval attempt 2, finding 4b: `.review/*` is gitignored, so the
"only a top-level Superseded annotation was added" claim for these 3
manual reports couldn't be verified from the commit. These `*.md.before`
files are the exact pre-annotation content of each report, so a reviewer
can diff a tracked file against the live gitignored one directly.

## How these were produced

Reconstructed from this session's own edit history: each report was
edited with a single `old_string`/`new_string` replacement that inserted
exactly 2 lines (a `> **Superseded** ...` blockquote plus a trailing blank
line) immediately after the title line, and touched nothing else. Removing
those same 2 lines (lines 3-4) from the current, live file reproduces the
pre-annotation content byte-for-byte — verified by re-inserting them back
into each `.before` copy and diffing against the live `.review` file
(empty diff both directions).

## Verifying

```sh
sha256sum .scratch/parity-runner/issues/08-review-report-before-images/*.before \
          .review/eval-report-plugin-init-doctor*.md
```

See `.scratch/parity-runner/issues/08-doctor-backfill.md`'s "Attempt 2"
section (finding 4b) for the exact before → after hash pairs.
