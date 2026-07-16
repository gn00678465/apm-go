# Backend Development Guidelines

> Best practices for backend development in this project.

---

## Overview

This directory contains guidelines for backend development. Fill in each file with your project's specific conventions.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Module organization and file layout | To fill |
| [Database Guidelines](./database-guidelines.md) | ORM patterns, queries, migrations | To fill |
| [Error Handling](./error-handling.md) | Error types, handling strategies | To fill |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, forbidden patterns | Active |
| [Logging Guidelines](./logging-guidelines.md) | Structured logging, log levels | To fill |
| [Install / Marketplace Contracts](./install-marketplace-contracts.md) | install & marketplace CLI invariants, validation matrices, documented deviations | Active |
| [Antigravity Target Contract](./antigravity-target-contract.md) | antigravity deploy target: serverUrl, explicit-only, agents primitive, agy verification gotchas | Active |
| [Compile Contract](./compile-contract.md) | `apm-go compile`: agents-family AGENTS.md generation, Build ID, idempotency, documented deviations | Active |
| [CLI Parity Notes](./cli-parity-notes.md) | `audit`/`normalize`/`validate`/`allowExecutables:` same-name and dev-only-extension notes (P0 parity quick wins) | Active |
| [Terminal UX Contract](./terminal-ux-contract.md) | `internal/ux` 門面：per-writer 著色、CanPrompt vs IsRich、串流保留、TTY 偵測、severity 對應、業務層禁 import ux | Active |
| [Dep Identity & Skill Subset Contract](./dep-identity-skill-subset-contract.md) | CanonicalRepoIdentity single normalization point (no scattered ToLower), per-dep SkillFilter + H6 invariant, effectiveSkillSubsets single computation point, skills: parse spec (Python parity), stale reconciliation per-skill-name rules, deliberate Python deviations | Active |

---

## How to Fill These Guidelines

For each guideline file:

1. Document your project's **actual conventions** (not ideals)
2. Include **code examples** from your codebase
3. List **forbidden patterns** and why
4. Add **common mistakes** your team has made

The goal is to help AI assistants and new team members understand how YOUR project works.

---

**Language**: All documentation should be written in **English**.
