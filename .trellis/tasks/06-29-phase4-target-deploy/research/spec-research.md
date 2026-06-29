# Phase 4 Spec Research: Primitive Sourcing and Target Deploy

- **Source**: OpenAPM v0.1 spec (`D:\Projects\apm-dev\apm\docs\src\content\docs\specs\openapm-v0.1.md`)
- **Companion**: Targets matrix (`D:\Projects\apm-dev\apm\docs\src\content\docs\reference\targets-matrix.md`)
- **Date**: 2026-06-29
- **Scope**: Sections 8.2-8.5, Section 4.2.1, and requirements req-pr-001 through req-pr-003, req-tg-001 through req-tg-003

---

## Section 4.2.1 -- Target canonical set

### Normative text

The canonical set of `target` identifiers registered by this specification at v0.1 is:

```
copilot, claude, cursor, codex, gemini, opencode, windsurf, agent-skills, all
```

### Key rules

- Legacy aliases `vscode` and `agents` MAY appear in input manifests and MUST be normalised to `copilot` when the manifest is rewritten.
- The internal fallback value `minimal` MUST NOT be set explicitly in a manifest; it is reserved for the auto-detection fallback (Section 8.4). In auto-detect context, `minimal` denotes the no-target-detected profile that emits `AGENTS.md` only; `all` denotes the union of every registered target.
- **[req-mf-005]** (Producer MUST): Reject any `target` value that is not (a) a member of the canonical set or a recognised alias, OR (b) a vendor-extension identifier matching `x-[a-z][a-z0-9-]*-[a-z][a-z0-9-]*` (the `x-<vendor>-<name>` form). The diagnostic MUST name the offending token.
- **[req-tg-004]** (Consumer MUST): Accept target identifiers matching the vendor-extension pattern at parse time and route detection/deployment/conformance to a vendor-registered handler. If no handler exists, emit a diagnostic naming the unsupported identifier; MUST NOT silently ignore it.

### Additional targets in companion (not in canonical set)

The targets-matrix companion lists `antigravity` and `kiro` as supported targets (with full detection signals and deploy directories), but these are NOT in the Section 4.2.1 canonical set. The companion also lists experimental targets: `copilot-cowork`, `copilot-app`, `openclaw`.

### Edge cases

- `vscode` -> normalise to `copilot`
- `agents` -> normalise to `copilot`
- `minimal` -> MUST NOT appear in manifest; internal-only fallback
- `all` -> union of every registered target
- Vendor extensions (`x-<vendor>-<name>`) are valid but require a handler

---

## Section 8.2 -- Discovery and source tracking (req-pr-001)

### Normative text (exact)

> **[req-pr-001]** A conforming **consumer** implementation MUST attach a source attribution to every discovered primitive. The attribution MUST be `local` for primitives sourced from the project's own `.apm/` directory and MUST be of the form `dependency:<name>` for primitives sourced from a resolved dependency.

### Observable outputs

1. Every primitive in the system carries a `source` field.
2. Local primitives (from `.apm/`) have `source = "local"`.
3. Dependency primitives have `source = "dependency:<name>"` where `<name>` is the dependency identity.

### Edge cases

- The spec does not define what `<name>` is precisely -- it implies the dependency name from the manifest (likely `<owner>/<repo>` or the alias).
- No mention of how to attribute primitives from virtual packages vs. whole-repo packages differently.

### Interactions with other reqs

- **req-pr-002**: Uses source attribution to determine local vs. dependency for conflict resolution.
- **req-pr-003**: Declaration order among dependencies determines priority for same-name conflicts.

---

## Section 8.3 -- Priority and conflict resolution (req-pr-002, req-pr-003)

### Normative text (exact)

> **[req-pr-002]** A conforming **consumer** implementation MUST cause local primitives to override dependency primitives of the same name and same primitive type. The conflict MUST be recorded in the consumer's diagnostic surface and MUST be inspectable by the user.

> **[req-pr-003]** A conforming **consumer** implementation MUST process dependencies in the order they are declared in the manifest (direct deps first, transitive deps appended in lockfile order). When two dependencies provide primitives with the same name and same type, the **first declared** dependency wins; later dependencies' versions MUST NOT replace the resolved primitive.

### Observable outputs

**req-pr-002:**
1. When a local primitive (source=local) and a dependency primitive share the same (name, type) pair, the local primitive wins.
2. The conflict MUST be recorded in diagnostics (user-inspectable).

**req-pr-003:**
1. Dependencies are processed in manifest declaration order.
2. Direct deps come before transitive deps.
3. Transitive deps are appended in lockfile order.
4. First-declared dependency wins for same (name, type) conflicts.
5. Later dependencies MUST NOT replace the resolved primitive.

### Edge cases

- Conflict between two transitive deps: lockfile order is the tiebreaker.
- Local always wins over dependency, regardless of declaration order.
- The spec says conflicts must be "inspectable by the user" -- this implies a diagnostic command or log output.
- The spec does not specify what happens if the same dependency provides two primitives of the same name and type (unlikely but unaddressed).

### Interactions with other reqs

- **req-pr-001**: Source attribution is what distinguishes local from dependency.
- **Section 7.4**: Package-level conflicts (same package identity at different versions) are governed by req-rs-001's tri-modal policy; Section 8.3 handles primitive-level conflicts (same name + type from different packages).
- **req-tg-002**: Deploy roots constrain where primitives can be written, which affects the physical outcome of conflict resolution.

### Priority order summary

```
1. Local primitives (.apm/ directory)           -- always win
2. First-declared direct dependency              -- wins over later deps
3. Later-declared direct dependencies            -- lose to earlier
4. Transitive dependencies (lockfile order)      -- lose to all direct deps
```

---

## Section 8.4 -- Target auto-detection predicates (req-tg-001)

### Normative text (exact)

> **[req-tg-001]** A conforming **consumer** implementation MUST honour the per-target detection predicate published in the registered OpenAPM Target Registry for every spec-registered target identifier and for every vendor-registered identifier ([req-tg-004]). Auto-detection MUST activate a target **only** when its registered predicate fires; no other filesystem signal MAY substitute for, or augment, the registered predicate. `agent-skills` MUST NOT be auto-detected; it MUST be selected explicitly via `--target agent-skills` or via the manifest's `target:` field. When no detection signal fires, the consumer MAY fall back to a `minimal` profile that emits `AGENTS.md` only.

### Observable outputs

1. Consumer checks filesystem for detection signals published in the Target Registry.
2. A target is activated ONLY when its specific predicate fires -- no custom signals allowed.
3. `agent-skills` is NEVER auto-detected; requires explicit selection.
4. When nothing is detected, fallback to `minimal` (AGENTS.md only) is permitted but not required.

### Detection signals from targets-matrix companion

| Target   | Signals (any one activates)                   |
|----------|-----------------------------------------------|
| claude   | `.claude/` directory, or `CLAUDE.md` file     |
| copilot  | `.github/copilot-instructions.md` file        |
| cursor   | `.cursor/` directory, or `.cursorrules` file   |
| codex    | `.codex/` directory                           |
| gemini   | `.gemini/` directory, or `GEMINI.md` file     |
| opencode | `.opencode/` directory                        |
| windsurf | `.windsurf/` directory                        |
| kiro     | `.kiro/` directory                            |

### Targets that are NEVER auto-detected

- `agent-skills` -- explicit only (`--target` or `targets:` in `apm.yml`)
- `antigravity` -- explicit only (shares `.agents/`, no unique signal)
- `copilot-cowork`, `copilot-app`, `openclaw` -- experimental, require enabling

### Resolution priority (from companion)

```
1. --target / --all on the command line
2. targets: in apm.yml
3. Auto-detection from filesystem signals
4. Fallback to copilot (companion says copilot; spec says minimal)
```

### Edge cases

- **Spec vs. companion discrepancy**: The companion says "If none of the above produce a target, the command falls back to `copilot`." The spec says the consumer MAY fall back to `minimal` that emits `AGENTS.md` only. The spec wins per Section 1.2.
- Multiple targets can be auto-detected simultaneously (e.g., both `.claude/` and `.cursor/` exist).
- `antigravity` is in the companion but not in the canonical set (Section 4.2.1); its handling may depend on the vendor-extension mechanism or an additive companion update.

### Interactions with other reqs

- **req-tg-002**: Once a target is detected/selected, deploy roots are determined by the Target Registry.
- **req-tg-004**: Vendor-extension targets must also be routed through a registered handler.
- **req-mf-005**: Target validation at parse time.

---

## Section 8.5 -- Deploy roots and skill convergence (req-tg-002, req-tg-003)

### Normative text (exact)

> **[req-tg-002]** A conforming **consumer** implementation MUST deploy primitives only under the deploy root(s) registered for the active target in the OpenAPM Target Registry. No target's installer MAY write files outside its registered root(s); writing outside the registered root MUST be treated as an implementation defect, not a runtime warning. When two targets register the same deploy root (for example two targets that both share `.agents/`), each target OWNS only the file-name patterns documented for that target in the Registry; `.agents/` is partitioned by subdirectory (`.agents/skills/`, `.agents/commands/`, `.agents/prompts/`, ...) so that distinct targets do not contend for the same on-disk patterns.

> **[req-tg-003]** A conforming **consumer** implementation MUST deploy skills to `.agents/skills/<name>/SKILL.md` for every target that supports the `skills` primitive type, unless the user has explicitly opted out of skill-convergence via the documented opt-out switch. This cross-tool convergence ensures a single skill bundle serves every harness without per-target duplication.

### Observable outputs

**req-tg-002:**
1. Primitives are deployed ONLY under the registered deploy root(s).
2. Writing outside registered roots is an implementation defect (not just a warning).
3. When targets share a deploy root (like `.agents/`), they are partitioned by subdirectory.

**req-tg-003:**
1. Skills deploy to `.agents/skills/<name>/SKILL.md` for ALL targets supporting skills.
2. Exception: user can opt out via a documented switch (companion says `--legacy-skill-paths` flag or `APM_LEGACY_SKILL_PATHS=1`).

### Deploy roots per target (from companion)

| Target       | Deploy root(s)                | Skills path                          |
|--------------|-------------------------------|--------------------------------------|
| copilot      | `.github/`                    | `.agents/skills/<name>/SKILL.md`     |
| claude       | `.claude/`                    | `.agents/skills/<name>/SKILL.md`     |
| cursor       | `.cursor/`                    | `.agents/skills/<name>/SKILL.md`     |
| codex        | `.codex/` + `.agents/`        | `.agents/skills/<name>/SKILL.md`     |
| gemini       | `.gemini/`                    | `.agents/skills/<name>/SKILL.md`     |
| antigravity  | `.agents/`                    | `.agents/skills/<name>/SKILL.md`     |
| opencode     | `.opencode/`                  | `.agents/skills/<name>/SKILL.md`     |
| windsurf     | `.windsurf/`                  | `.windsurf/skills/<name>/SKILL.md`   |
| kiro         | `.kiro/`                      | `.kiro/skills/<name>/SKILL.md`       |
| agent-skills | `.agents/`                    | `.agents/skills/<name>/SKILL.md`     |

### Skill convergence exceptions (from companion)

- **Claude, Windsurf, and Kiro** keep target-native skill directories by default (per companion). However, req-tg-003 says skills MUST go to `.agents/skills/` for "every target that supports the skills primitive type". There appears to be a tension; the companion says Claude deploys to `.agents/skills/<name>/SKILL.md` (convergent) while Windsurf uses `.windsurf/skills/` and Kiro uses `.kiro/skills/`. The spec (req-tg-003) would require convergence unless opted out. The companion (being non-normative) may reflect the reference implementation's current behavior including opt-outs.

### File conventions per target (from companion, selected)

**copilot:**
- instructions: `.github/instructions/<name>.instructions.md`
- prompts: `.github/prompts/<name>.prompt.md`
- agents: `.github/agents/<name>.agent.md`
- skills: `.agents/skills/<name>/SKILL.md`
- hooks: `.github/hooks/<name>.json`

**claude:**
- instructions: `.claude/rules/<name>.md`
- agents: `.claude/agents/<name>.md`
- commands: `.claude/commands/<name>.md`
- skills: `.agents/skills/<name>/SKILL.md`
- hooks: merged into `.claude/settings.json`

**cursor:**
- instructions: `.cursor/rules/<name>.mdc`
- agents: `.cursor/agents/<name>.md`
- commands: `.cursor/commands/<name>.md`
- skills: `.agents/skills/<name>/SKILL.md`
- hooks: `.cursor/hooks.json`

**codex:**
- agents: `.codex/agents/<name>.toml`
- skills: `.agents/skills/<name>/SKILL.md`
- hooks: `.codex/hooks.json`

### Edge cases

- `.agents/` is shared by multiple targets. Partitioning is by subdirectory, not by file.
- Opt-out of skill convergence: `--legacy-skill-paths` or `APM_LEGACY_SKILL_PATHS=1`.
- Windsurf and Kiro are noted to "keep target-native skill directories" in the companion, which may mean they are the targets that default to opted-out.
- codex has TWO deploy roots (`.codex/` + `.agents/`).
- `agent-skills` target only supports the `skills` primitive type; it exists purely for cross-client skill deployment.

### Interactions with other reqs

- **req-pr-002/003**: Conflict resolution happens before deployment; deploy roots determine WHERE the winning primitive lands.
- **req-tg-001**: Detection determines WHICH targets are active, and thus which deploy roots apply.
- **req-tg-004**: Vendor-extension targets must register their own deploy roots.
- **Section 10.7**: Deploy root constraints prevent hostile transitive deps from writing outside their footprint.

---

## Cross-requirement interaction matrix

| Req | Depends on | Depended on by |
|-----|-----------|----------------|
| req-pr-001 (source attribution) | -- | req-pr-002 (uses source to distinguish local vs. dep) |
| req-pr-002 (local wins) | req-pr-001 | req-tg-002 (deploy root determines physical output) |
| req-pr-003 (declaration order) | -- | req-tg-002, req-tg-003 (deploy after conflict resolution) |
| req-tg-001 (detection predicates) | targets-matrix companion | req-tg-002 (active targets determine deploy roots) |
| req-tg-002 (deploy roots) | req-tg-001, targets-matrix companion | req-tg-003 (skills are a special case of deploy) |
| req-tg-003 (skill convergence) | req-tg-002 | -- |
| req-tg-004 (vendor extensions) | -- | req-tg-001 (vendor targets also need detection) |
| req-mf-005 (target validation) | Section 4.2.1 canonical set | req-tg-001 (validated targets get detected) |

---

## Primitive types (Section 8.1, reference)

Seven primitive types: `instructions`, `prompts`, `agents`, `skills`, `commands`, `hooks`, `mcp`.

Package layout forms:
1. **APM package** (`.apm/` directory) -- typed subdirectories (`.apm/skills/`, `.apm/agents/`, etc.)
2. **Skill bundle** (`SKILL.md` at root) -- whole directory copied to `<deploy>/skills/<name>/`
3. **Skill collection** (`skills/<name>/SKILL.md` nested) -- each nested skill promoted
4. **Plugin collection** (`plugin.json` / `.claude-plugin/`) -- mapped per plugin manifest

---

## Implementation checklist for Phase 4

Based on the spec requirements, a conforming consumer implementation needs:

### Primitive sourcing (req-pr-001, req-pr-002, req-pr-003)
- [ ] Attach `source: "local"` or `source: "dependency:<name>"` to every discovered primitive
- [ ] Local primitives override dependency primitives of same (name, type)
- [ ] Record conflicts in diagnostic surface (inspectable by user)
- [ ] Process dependencies in manifest declaration order (direct first, transitive in lockfile order)
- [ ] First-declared dependency wins for same (name, type) conflicts

### Target detection (req-tg-001, req-tg-004)
- [ ] Implement per-target detection predicates per the Target Registry
- [ ] Support priority: --target > manifest targets: > auto-detect > fallback
- [ ] NEVER auto-detect `agent-skills` (explicit only)
- [ ] Accept vendor-extension target identifiers (`x-<vendor>-<name>`)
- [ ] Route vendor targets to registered handler or emit diagnostic

### Target deployment (req-tg-002, req-tg-003)
- [ ] Deploy ONLY under registered deploy root(s) for active target
- [ ] Treat writes outside registered roots as implementation defect
- [ ] Partition shared roots (`.agents/`) by subdirectory
- [ ] Deploy skills to `.agents/skills/<name>/SKILL.md` by default (convergence)
- [ ] Support opt-out of skill convergence (`--legacy-skill-paths` / `APM_LEGACY_SKILL_PATHS=1`)

### Target validation (req-mf-005)
- [ ] Validate target values against canonical set + vendor-extension pattern
- [ ] Normalise legacy aliases: `vscode` -> `copilot`, `agents` -> `copilot`
- [ ] Reject `minimal` if explicitly set in manifest
