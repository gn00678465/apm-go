# Pack a plugin

`pack` turns a plugin project into a distributable bundle: a directory `build/<name>-<version>/` holding `plugin.json`, or an archive of it. It can report what it would write without writing, and it can emit its result as JSON. `plugin validate` checks a `plugin.json` and writes nothing.

## Sub-features

- `plugin-validate` reports five checks and leaves the project untouched.
- `pack-bundle` writes the bundle directory and a `.claude-plugin/plugin.json` manifest.
- `pack-archive` writes a `.zip` (default) or a `.tar.gz`.
- `pack-dry-run` lists what it would write and writes nothing.
- `pack-json` puts a JSON document on stdout and status lines on stderr.

## How to get to it (user POV)

- Run `apm-go plugin validate <dir>`.
- Run `apm-go pack` in a plugin project.
- Run `apm-go pack --archive -o <dir>`, optionally with `--archive-format tar.gz`.
- Run `apm-go pack --dry-run -o <dir>`.
- Run `apm-go pack --json`.

## Driving it with verify.sh

Preconditions:

- `sandbox/work/plug` was created by `$V run RUN plugin-init -- plugin init plug --yes --target claude` (see [init.md](./init.md)).
- Drives of `pack` use `--cd plug`; `plugin validate` runs from `sandbox/work`.

- **plugin-validate.** Run `$V run RUN plugin-validate -- plugin validate plug`. Exit code `0`. stdout starts ` > Validating plugin 'plug/plugin.json'...`, lists `Structure`, `Name`, `Fields`, `Paths`, `Unrecognized` each as `passed`, and ends ` i Summary: 5 passed, 0 warnings, 0 errors`. stderr is empty. `plugin-validate.tree` equals `plugin-init.tree`.
- **pack-dry-run.** Run `$V run RUN pack-dry --cd plug -- pack --dry-run -o dry`. Exit code `0`. stdout has ` i Would write plugin manifest to …/plug/.claude-plugin/plugin.json` and `dry-run: Would pack 1 file(s) -> …/plug/dry/plug-0.1.0`. The tree is unchanged: no `dry/`, no `.claude-plugin/`.
- **pack-bundle.** Run `$V run RUN pack --cd plug -- pack`. Exit code `0`. stdout has ` + Generated plugin manifest: …/plug/.claude-plugin/plugin.json` and ` + Packed 1 file(s) -> build/plug-0.1.0`. The tree gains `./work/plug/build/plug-0.1.0/plugin.json` and `./work/plug/.claude-plugin/plugin.json`.
- **pack-archive.** Run `$V run RUN pack-zip --cd plug -- pack --archive -o dist`. Exit code `0`. stdout has ` + Packed 1 file(s) -> dist/plug-0.1.0.zip` with a byte count. The tree gains `./work/plug/dist/plug-0.1.0.zip`. With `--archive-format tar.gz -o dist2` the file is `dist2/plug-0.1.0.tar.gz`.
- **pack-json.** Run `$V run RUN pack-json --cd plug -- pack --json`. Exit code `0`. `pack-json.out` is one JSON document with `"ok": true` and the keys `dry_run`, `warnings`, `errors`, `metadata_enrichment`, `marketplace`, `bundle`, `plugin_manifests`, `version_alignment`, `drift`; it parses (`jq . RUN/evidence/pack-json.out`). The ` ! ` status lines are in `pack-json.err`, not in `.out`.
- **Proof.** The `.out`, `.err`, `.rc` of each step, the tree diff of each writing step, and the unchanged tree of `plugin-validate` and `pack-dry-run`.

## Gotchas

- Drive `pack-dry-run` before `pack-bundle`. After a real pack, `.claude-plugin/plugin.json` exists and later runs print ` ! … already exists; skipping plugin.json generation` instead of the `Generated` line.
- Every `pack` in the scaffold prints ` ! No 'license:' field in apm.yml; …`. It is a warning on stdout (stderr under `--json`), exit code stays `0`.
- Under `--json` an empty stdout or a status line in stdout is a failure of the contract even when the exit code is `0`.
- `ls` hides `.claude-plugin/`. Use the `.tree` file to see what was written.
- A project with a `marketplace:` block takes the marketplace path of `pack`; that is [marketplace.md](./marketplace.md).
- The release gates (`--check-versions`, `--check-clean`) and `--strict-metadata` are not covered by this file.
