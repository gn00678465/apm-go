# Dependency Identity & Skill Subset Contract

> Executable contracts introduced by task `07-16-install-parity-bugfix`
> (BUG-2 `--skill` subset amnesia + BUG-1 case-duplicate deps). Every future
> change touching dep comparison, `--skill`, apm.yml `skills:`, or lockfile
> `skill_subset` MUST follow these invariants. Full rationale:
> `.trellis/tasks/archive/*/07-16-install-parity-bugfix/` (design.md,
> research/codex-plan-review.md, research/bug2-python-baseline.md).

---

## 1. Canonical identity — single normalization point

### Signatures

```go
// internal/manifest/canonical_identity.go
func CanonicalRepoIdentity(ref *DependencyReference) string

// internal/deploy/deploy.go — identity + "/" + VirtualPath when set
func CanonicalDepKey(ref *manifest.DependencyReference) string

// cmd/apm-go/install.go — bridges for other input shapes
func resolvedDepCanonicalKey(dep resolver.ResolvedDep) string // re-parses RepoURL via ParseDepString (host-aware)
func canonicalIdentityForDepString(s string) string           // ParseDepString + normalizeLocalDep
func entryCanonicalIdentity(entry *yamllib.Node) string       // WHOLE dict via ParseDepDict (path: included)
```

### Contract

- **GitHub host** (explicit `github.com` or empty/default Host): owner/repo case-folded,
  host component dropped → `"owner/repo"`.
- **Self-hosted host**: host lowercased, owner/repo case **preserved** → `"git.internal/Acme/Repo"`.
- **Non-git sources** get a `%q`-quoted prefixed namespace (collision-proof):
  `registry:"<name>":"<id>"`, `marketplace:"<mkt>":"<plugin>"`.
- **Selectors NEVER participate**: ref/version/alias are excluded and never lowercased
  (git refs are case-sensitive). VirtualPath is NOT part of repo identity but IS part of
  `CanonicalDepKey` (a monorepo sub-package is a distinct dependency).
- Local git path (`git: ./path`) keeps the path verbatim; local/parent refs return `""`.

### Forbidden

- **NEVER call `strings.ToLower`/`EqualFold` on a dep key at a call site.** Route every
  comparison (resolver dedup `bfsKey`, `requestedKeys`/`existing`, manifest entry lookup,
  lockfile lookup, deploy filter keys) through the functions above. Scattered folding is
  exactly the split-brain that caused BUG-1.
- **NEVER feed a raw RepoURL string into the bare-literal branch when a structured parse
  is possible** — `resolvedDepCanonicalKey` exists because the bare branch blanket-lowercases,
  which diverges from the manifest side on self-hosted hosts (final-gate HIGH).
- Display/serialization always keeps the **first-declared raw spelling**
  (`ResolvedDep.Key` = displayKey; apm_modules dir names; apm.yml git values).

### Tests required

`internal/manifest/canonical_identity_test.go` (equivalent forms, case-fold, self-hosted
preserve, cross-host non-collision, namespace unambiguity, selector exclusion) and
`cmd/apm-go` `TestResolvedDepCanonicalKey_SelfHostedPreservesCase`,
`TestInstall_CaseFoldDedup*`. Any new identity consumer must add an equality test
against `CanonicalDepKey` of the same logical dep expressed another way.

---

## 2. `deploy.SkillFilter` — per-dep map + H6 invariant

```go
type SkillFilter struct {
	Subsets map[string][]string // key = CanonicalDepKey; value = skill-name whitelist
}
```

- **Key absent = deploy every skill.** That is the ONLY representation of "no filter".
- **H6 invariant: a value must never be an empty slice** — construction must reject/skip
  empty unions (an empty slice would silently deploy zero skills). `effectiveSkillSubsets`
  guards this; `validateNewSkillNames` rejects blank `--skill` values up front.
- Applies to `TypeSkills` primitives only; prod (`ParsedDeps`) and dev (`ParsedDevDeps`)
  both go through the same `depCanonKeys` lookup; local primitives (empty dep key) and
  transitive deps absent from the map are never affected.

---

## 3. `effectiveSkillSubsets` — the single computation point (C2)

```go
// cmd/apm-go/install.go
func effectiveSkillSubsets(m *manifest.Manifest, requestedKeys map[string]bool,
	cliSubset []string) map[string][]string
```

**The same returned map feeds all three account states**: apm.yml persistence
(`persistPackagesToManifest`), lockfile `skill_subset` (`buildLockfile`, including deps NOT
named this call), and `deploy.SkillFilter`. `update` computes it too
(`effectiveSkillSubsets(m, nil, nil)` = persisted-only). No call site may re-derive
"the effective subset" from raw CLI flags on its own.

Rules (Python issue #1771 parity):
1. Persisted `SkillSubset != nil` seeds the map.
2. CLI `--skill` (non-wildcard) **unions additively** into requestedKeys' entries
   (trim/dedupe/sort — `unionSortedSkills`).
3. Any `'*'` in cliSubset (even mixed with names) = RESET: requestedKeys' entries are
   **deleted** from the map; other deps untouched.

### Validation & error matrix

| Input | Behavior |
|---|---|
| `--skill <unknown-name>` (new, matches no requested dep's skills) | error, exit 1, **before** any apm.yml/lockfile/target write (`validateNewSkillNames`; apm_modules cache refresh is out of atomicity scope, by design) |
| `--skill " "` (blank after trim) | error (H6 guard) |
| persisted name vanished upstream | non-fatal `deploy.Run` diag warning; keep, never prune |
| `skills: null` in apm.yml | = key absent (Python parity: `entry.get("skills") is None`) |
| `skills: []` / non-string / `.` `..` `/` `\` `:` names | parse error (`ParseDepDict`, mirrors reference.py:915-945 + colon hardening) |
| `skills:` on registry/marketplace/path/parent dict | parse error (never silently dropped) |

---

## 4. apm.yml entry updates — key surgery only

`setEntrySkillSubset` must modify **only the `skills:` pair** of an existing mapping entry:
- sibling fields (`ref:`, `path:`, `type:`, `alias:`) are preserved verbatim (final-gate
  HIGH: rebuilding as `{git, skills}` silently dropped version pins);
- collapse to scalar form ONLY when `git:` is the sole remaining key after a RESET;
- existing entries are located by `entryCanonicalIdentity` (whole-dict parse, so
  `{git: repo, path: sub}` is never mis-hit by a base-repo positional);
- the original persisted git spelling wins (first-declared).

Wrong vs correct:

```go
// WRONG — loses ref:/path: siblings
apmSeq.Content[idx] = newGitSkillEntry(gitVal, subset)

// CORRECT — replace/insert/remove just the skills key on the existing node
entry.Content[j+1] = newSkillSeqNode(subset)
```

---

## 5. Stale-skill reconciliation — per-skill-name eligibility

`reconcileStaleSkillDeployments(existingLock, newLock, projectDir)`:

- Eligibility is **per skill name**: an old deployed path is a deletion candidate only when
  (a) it parses as a skill path (`skillNameFromDeployPath`: segment after `skills/`), AND
  (b) that name is absent from the dep's **fresh** `SkillSubset`, AND (c) the dep has an
  active fresh subset at all. A `--target` selection change must never trigger deletion.
- Claim check stays **global across the whole fresh lockfile** (incl. `LocalDeployedFiles`):
  a path another dep/local now owns is never deleted.
- Non-skill paths and the local bucket are NEVER pruned by this mechanism.
- Deletion goes through `deploy.RemoveDeployedFiles` (old-hash verification): user-modified
  files are kept + warned.
- Path equality uses `normalizeDeployPath` (explicit `\`→`/`; `filepath.ToSlash` is a no-op
  on Unix and must not be used for lockfile-path normalization).

Tests: `TestInstall_StaleSkillReconciliation*` (3), `TestInstall_SkillSubsetThreeRepos`.

---

## 6. Deliberate deviations from Python apm (do NOT "fix" back)

| Topic | Python 0.21.0 | apm-go | Why |
|---|---|---|---|
| Same-dep re-install deploy set | deploys only current CLI names, stale-cleans the union remainder (P-D1: transient manifest↔fs mismatch) | deploys the full effective **union** immediately | single computation point; no transient inconsistency |
| Unknown `--skill` name | exit 0, silently persisted forever (P-D2) | exit 1, zero account-state change | "never pollute the ledger" principle (prd B2-6) |

Evidence: `research/bug2-python-baseline.md` (versions, SHAs, transcripts).
