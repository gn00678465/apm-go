# Marketplace

A marketplace author declares packages in the `marketplace:` block of `apm.yml` and runs `pack` to produce `.claude-plugin/marketplace.json`. A consumer registers a marketplace by path or URL, then lists, browses and searches it. The registry of registered marketplaces is a file in the user's config directory.

## Sub-features

- `mk-init` creates `apm.yml` with a `marketplace:` block.
- `mk-package-edit` removes and adds a package entry.
- `mk-pack` writes `marketplace.json`.
- `mk-register` adds the marketplace to the user registry.
- `mk-read` lists, browses and searches the registered marketplace.
- `mk-unregister` removes it from the registry.

## How to get to it (user POV)

- Run `apm-go marketplace init --name <name> --owner <owner>` in an empty directory.
- Run `apm-go marketplace package add <path> --name <name>` and `apm-go marketplace package remove <name> --yes`.
- Run `apm-go pack` in the marketplace directory.
- Run `apm-go marketplace add <path> --name <name>`, `marketplace list`, `marketplace browse <name>`, `marketplace remove <name> --yes`.
- Run `apm-go search <query>@<marketplace>`.

## Driving it with verify.sh

Preconditions:

- `sandbox/work/plug` exists from `plugin init plug --yes --target claude` and has not been packed (no `build/`, `dist/`, `.claude-plugin/`).
- `sandbox/work/mk` is an empty directory.

- **mk-init.** Run `$V run RUN mk-init --cd mk -- marketplace init --name mk --owner me`. Exit code `0`. stdout has ` + Created apm.yml with 'marketplace:' block` and a `Next Steps` panel (a box 80 columns wide with four numbered steps; step 3 wraps onto a second line). The tree gains `./work/mk/apm.yml`; it contains a package named `example-package`.
- **mk-package-edit.** Run `$V run RUN mk-rm-example --cd mk -- marketplace package remove example-package --yes`; stdout is ` + Removed package 'example-package'`. Copy the plugin in: `mkdir -p RUN/sandbox/work/mk/packages && cp -r RUN/sandbox/work/plug RUN/sandbox/work/mk/packages/demo`. Run `$V run RUN mk-pkg-add --cd mk -- marketplace package add ./packages/demo --name demo --category tools --no-verify`; exit code `0`, stdout has ` + Added package 'demo' from ./packages/demo`. `apm.yml` lists `demo` and no `example-package`.
- **mk-pack.** Run `$V run RUN mk-pack --cd mk -- pack`. Exit code `0`. stdout has ` + Built marketplace.json [claude] (1 package(s)) -> …/mk/.claude-plugin/marketplace.json`. The file has `"name": "mk"` and one plugin with `"name": "demo"`, `"version": "0.1.0"`, `"category": "tools"`, `"source": "./packages/demo"`.
- **mk-register.** Run `$V run RUN mk-add -- marketplace add ./mk --name mk` (from `sandbox/work`). Exit code `0`, stdout has ` + Marketplace 'mk' registered (1 plugins)`. The tree gains `./home/.apm/marketplaces.json`, which lists `mk` with a `file://` URL of the sandbox path.
- **mk-read.** Run `$V run RUN mk-list -- marketplace list`; the `Registered Marketplaces` table has a row `mk`. Run `$V run RUN mk-browse -- marketplace browse mk`; the `Plugins in 'mk'` table has a row `demo` with install handle `demo@mk`. Run `$V run RUN search -- search demo@mk`; the `Search Results: 'demo' in mk` table has the row `demo`. Each exits `0` and leaves the tree unchanged.
- **mk-unregister.** Run `$V run RUN mk-remove -- marketplace remove mk --yes`. Exit code `0`, stdout is ` + Marketplace 'mk' removed`. A following `marketplace list` has no row `mk`.
- **Proof.** The `.out` / `.rc` of each step, `marketplace.json` and `marketplaces.json` read from the sandbox, and the tree diff of `mk-add` showing the registry file under `./home/.apm/`, never outside the sandbox.

## Gotchas

- The registry lives under `APM_CONFIG_DIR`, which the helper points at `sandbox/home/.apm`. A registry file appearing in the real `~/.apm` means the drive escaped the sandbox; stop and report it.
- `marketplace package add` without `--no-verify` checks the source, which for a remote source needs the network.
- `mk-pkg-add` may print ` ! packages: block structure required rewriting the whole list; …` before the `Added` line, on stdout. It is a warning, exit code `0`.
- `marketplace list` prints the absolute sandbox path in the `Source` column; the table is as wide as that path.
- Copy `plug` before it is packed. A copy that carries `build/` or `.claude-plugin/` changes what `pack` reports.
- `marketplace check`, `outdated`, `package update` and `audit` work on remote, pinned packages. Their network-free contract is pinned by `tools/realexec.sh`; they have no recipe here.
