# Journal - Madao (Part 1)

> AI development session journal
> Started: 2026-06-27

---



## Session 1: Phase 0: YAML safe-loader and round-trip core

**Date**: 2026-06-27
**Task**: Phase 0: YAML safe-loader and round-trip core
**Branch**: `feat/phase-0-yaml-core`

### Summary

Implemented Phase 0 YAML core: SafeLoad (anchor/alias/custom-tag rejection, multi-doc rejection), SafeDump (byte-exact round-trip), IsVendorExtKey, CLI validate/normalize commands. Switched from yaml.v3 to yaml.v4. All 40 tests pass, 87.8% coverage, 21/21 A/B tests pass vs Python apm. Review fixes applied (SF-001~SF-004).

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `0deb4af` | (see git log) |
| `54df1fa` | (see git log) |
| `89bdda3` | (see git log) |
| `010f285` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 2: Phase 1: Manifest parsing, validation, init, and review fixes

**Date**: 2026-06-28
**Task**: Phase 1: Manifest parsing, validation, init, and review fixes
**Branch**: `feat/phase-1-manifest`

### Summary

Implemented Phase 1 manifest layer: ParseManifest with 21 reqs (mf-001~021, tg-004, sc-006), DependencyReference ABNF parser, MCP validation, marketplace source validation, placeholder recognition, target vocabulary with aliases and auto-detection. Rewrote apm init as full interactive flow (prompts, numbered toggle target selector, confirmation panel, --yes mode, filesystem signal detection). Review fixes applied (init YAML safety, target:all false positive, insecure bool, CLI tests). 46/46 A/B tests pass, 85.7% manifest coverage.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `2b2daad` | (see git log) |
| `b629604` | (see git log) |
| `8fc73aa` | (see git log) |
| `b34b239` | (see git log) |
| `3a265e1` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 3: Phase 1: Manifest parsing, validation, init, and review fixes

**Date**: 2026-06-28
**Task**: Phase 1: Manifest parsing, validation, init, and review fixes
**Branch**: `feat/phase-1-manifest`

### Summary

Implemented Phase 1 manifest layer: ParseManifest with 21 reqs (core fields, target validation, registries, dep ABNF parsing, MCP validation, marketplace, placeholder recognition). Rewrote init with full interactive flow (metadata prompts, target toggle, auto-detection, confirmation). Three review rounds (opus-4.8, gpt-5.4, sonnet-4.6) with all findings fixed. 46/46 A/B tests pass vs Python apm.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `2b2daad` | (see git log) |
| `b629604` | (see git log) |
| `8fc73aa` | (see git log) |
| `b34b239` | (see git log) |
| `3a265e1` | (see git log) |
| `14b9602` | (see git log) |
| `0d7650e` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 4: Phase 2: Dependency Resolution Engine

**Date**: 2026-06-29
**Task**: Phase 2: Dependency Resolution Engine
**Branch**: `main`

### Summary

實作依賴解析引擎：semver wrapper (deps.dev/util/semver, 24/24 oracle)、BFS+fixpoint resolver、diamond tri-modal 衝突偵測、lock replay、why 診斷、update 邏輯。經 gpt-5.4+opus-4.6 review 後修正 9 項問題。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a9b8311` | (see git log) |
| `3af3370` | (see git log) |
| `f38164d` | (see git log) |
| `c2c944a` | (see git log) |
| `51dc086` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 5: Phase 2+3: Resolver Engine + Lockfile Write + Install CLI

**Date**: 2026-06-29
**Task**: Phase 2+3: Resolver Engine + Lockfile Write + Install CLI
**Branch**: `main`

### Summary

Phase 2: 依賴解析引擎（semver/resolver/lockfile read）14 reqs + 9 項 review 修正。Phase 3: lockfile 序列化/hash/tree_sha256/frozen install/install CLI 18 reqs + 10 項 review 修正。經 gpt-5.4/opus-4.6/opus-4.8/gemini-3.5 multi-model review。

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `a9b8311` | (see git log) |
| `3af3370` | (see git log) |
| `f38164d` | (see git log) |
| `c2c944a` | (see git log) |
| `514df57` | (see git log) |
| `0a2e3b2` | (see git log) |
| `021f8c3` | (see git log) |
| `ffd959a` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete
