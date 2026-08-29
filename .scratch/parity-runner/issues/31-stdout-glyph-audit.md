# Ticket 31 stdout glyph and wording audit

Oracle pin: `b75a02b1cfab3ffa5e1952916045b6d5374090ae`.

This inventory covers every qualified `ux.Success`, `ux.Check`, `ux.Sparkle`,
`ux.Gear`, `ux.Info`, `ux.Running`, `ux.Warn`, `ux.Error`, `ux.Section`,
`ux.BulletList`, and `ux.Plain` production call expression in
`cmd/apm-go/*.go` and `internal/**/*.go`, excluding tests and printer
function definitions. The baseline contained 244 callsites: 29 Success,
28 BulletList, 87 Info, 61 Warn, 18 Error, 8 Section, 5 Sparkle, 3 Running,
3 Plain, 1 Gear, and 1 Check. The 28 BulletList sites now use the
stream-facing `ux.List` continuation renderer; the interactive TUI
`BulletList` remains available only to non-stream surfaces/tests.

The pre-audit defects were the 29 Success/28 BulletList TUI emissions, the
marketplace registration channel/quote mismatch, the marketplace add local
probe error wording, and several per-command success-symbol/quote mismatches.
All rows below describe the post-change contract. `match` means the output
channel, Oracle glyph, and reviewed message wording are aligned, allowing the
project's documented `apm-go` binary-name deviation. `apm-go-only` means the
Go helper has no direct Oracle logger callsite; it is still required to avoid
TUI-only glyphs on streams.

| apm-go site | Oracle site / logger method | Oracle glyph | Oracle words (first source line) | status |
|---|---|---|---|---|
| `cmd/apm-go/audit.go:80 ux.Error` | `commands/audit.py:276-315 (audit)` | `[x]` | `cmd.ErrOrStderr(), "content-integrity violation: %s expected %s, observed %s",` | match |
| `cmd/apm-go/audit.go:91 ux.Success` | `commands/audit.py:276-315 (audit)` | `[*]` | `cmd.OutOrStdout(), "audit: %d deployed files verified", count)` | match |
| `cmd/apm-go/audit.go:148 ux.BulletList` | `commands/audit.py:276-315 (audit)` | `(none;  - continuation)` | `w, items)` | match |
| `cmd/apm-go/audit_content.go:55 ux.Success` | `commands/audit.py:276-315 (audit)` | `[*]` | `out, "audit --content: %d file(s) scanned, no hidden characters", len(paths))` | match |
| `cmd/apm-go/audit_content.go:65 ux.Error` | `commands/audit.py:276-315 (audit)` | `[x]` | `errOut, "%s", msg)` | match |
| `cmd/apm-go/audit_content.go:67 ux.Warn` | `commands/audit.py:276-315 (audit)` | `[!]` | `errOut, "%s", msg)` | match |
| `cmd/apm-go/audit_content.go:69 ux.Info` | `commands/audit.py:276-315 (audit)` | `[i]` | `errOut, "%s", msg)` | match |
| `cmd/apm-go/audit_content.go:83 ux.Info` | `commands/audit.py:276-315 (audit)` | `[i]` | `out, "audit --content: %d file(s) scanned, %d info-level finding(s) (no action needed)",` | match |
| `cmd/apm-go/compile.go:52 ux.Error` | `commands/compile/cli.py:383-444 (compile)` | `[x]` | `os.Stderr, "No instruction files found in .apm/ directory")` | match |
| `cmd/apm-go/compile.go:64 ux.Error` | `commands/compile/cli.py:383-444 (compile)` | `[x]` | `os.Stderr, "%s", msg)` | match |
| `cmd/apm-go/compile.go:82 ux.BulletList` | `commands/compile/cli.py:383-444 (compile)` | `(none;  - continuation)` | `os.Stdout, items)` | match |
| `cmd/apm-go/compile.go:84 ux.Success` | `commands/compile/cli.py:383-444 (compile)` | `[*]` | `os.Stdout, "Compiled %d instruction(s) to %s", result.InstructionCount, result.Path)` | match |
| `cmd/apm-go/compile.go:86 ux.Info` | `commands/compile/cli.py:383-444 (compile)` | `[i]` | `os.Stdout, "No changes detected; preserving existing AGENTS.md for idempotency")` | match |
| `cmd/apm-go/compile.go:99 ux.Error` | `commands/compile/cli.py:383-444 (compile)` | `[x]` | `os.Stderr, "Not an APM project - no apm.yml found")` | match |
| `cmd/apm-go/doctor.go:282 ux.Plain` | `commands/doctor.py:1-200 (diagnostics heading)` | `(none; structural/plain)` | `w, "")` | match |
| `cmd/apm-go/doctor.go:283 ux.Section` | `commands/doctor.py:1-200 (diagnostics heading)` | `(none)` | `w, "Environment Diagnostics")` | match |
| `cmd/apm-go/experimental.go:41 ux.Success` | `commands/experimental.py:133 (success(sparkles))` | `[*]` | `cmd.OutOrStdout(), "Enabled experimental feature: %s", args[0])` | match |
| `cmd/apm-go/experimental.go:43 ux.Info` | `commands/experimental.py:281 (info/progress(info))` | `[i]` | `cmd.OutOrStdout(), "%s", f.Hint)` | match |
| `cmd/apm-go/experimental.go:57 ux.Success` | `commands/experimental.py:281 (success(sparkles))` | `[*]` | `cmd.OutOrStdout(), "Disabled experimental feature: %s", args[0])` | match |
| `cmd/apm-go/init.go:195 ux.Running` | `apm-go-only (no Oracle callsite for this helper)` | `[>]` | `os.Stdout, "Created project directory: %s", pn)` | apm-go-only |
| `cmd/apm-go/init.go:235 ux.Warn` | `apm-go-only (no Oracle callsite for this helper)` | `[!]` | `os.Stderr, "%s", notice)` | apm-go-only |
| `cmd/apm-go/init.go:236 ux.Info` | `apm-go-only (no Oracle callsite for this helper)` | `[i]` | `os.Stderr, "--yes specified, overwriting: %s", rendered)` | apm-go-only |
| `cmd/apm-go/init.go:363 ux.Running` | `apm-go-only (no Oracle callsite for this helper)` | `[>]` | `os.Stdout, "Initializing APM project: %s", name)` | apm-go-only |
| `cmd/apm-go/init.go:374 ux.Warn` | `apm-go-only (no Oracle callsite for this helper)` | `[!]` | `os.Stderr, "%s", msg)` | apm-go-only |
| `cmd/apm-go/init.go:499 ux.Plain` | `apm-go-only (no Oracle callsite for this helper)` | `(none; structural/plain)` | `w, "    Created Files")` | apm-go-only |
| `cmd/apm-go/init.go:501 ux.Plain` | `apm-go-only (no Oracle callsite for this helper)` | `(none; structural/plain)` | `w, "")` | apm-go-only |
| `cmd/apm-go/install.go:228 ux.Info` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[i]` | `os.Stderr, "CI environment detected, defaulting to frozen install")` | match |
| `cmd/apm-go/install.go:458 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "%s: ref %q conflicts with already-declared %q (%s#%s) -- keeping the first-declared ref",` | match |
| `cmd/apm-go/install.go:735 ux.Success` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[*]` | `os.Stdout, "Frozen install: all dependencies pinned and verified")` | match |
| `cmd/apm-go/install.go:760 ux.Info` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[i]` | `os.Stdout, "No dependencies to install")` | match |
| `cmd/apm-go/install.go:762 ux.Error` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[x]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/install.go:888 ux.Error` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[x]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/install.go:911 ux.BulletList` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `(none;  - continuation)` | `w, items)` | match |
| `cmd/apm-go/install.go:1017 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "Bundle has no apm.lock.yaml -- skipping integrity check. "+` | match |
| `cmd/apm-go/install.go:1020 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "Bundle has an apm.lock.yaml but no 'pack:' metadata section -- skipping integrity check.")` | match |
| `cmd/apm-go/install.go:1022 ux.Error` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[x]` | `os.Stderr, "Bundle integrity check failed:")` | match |
| `cmd/apm-go/install.go:1027 ux.BulletList` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `(none;  - continuation)` | `os.Stderr, items)` | match |
| `cmd/apm-go/install.go:1033 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/install.go:1036 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stdout, "No active targets resolved -- nothing will be deployed. Pass --target to select one explicitly.")` | match |
| `cmd/apm-go/install.go:1041 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "%s", warning)` | match |
| `cmd/apm-go/install.go:1053 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/install.go:1057 ux.Info` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[i]` | `os.Stdout, "No files deployed from local bundle")` | match |
| `cmd/apm-go/install.go:1065 ux.Success` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[*]` | `os.Stdout, "Installed %d file(s) from local bundle %s", len(result.Files), bundleArg)` | match |
| `cmd/apm-go/install.go:1225 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "apm.yml declares %q more than once (case/spelling variants of the same repository) -- consider removing the duplicate entry", dep.RepoURL)` | match |
| `cmd/apm-go/install.go:1718 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/install.go:1727 ux.Info` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[i]` | `os.Stdout, "Targets: %s  (source: %s)", strings.Join(targets, ", "), targetSource)` | match |
| `cmd/apm-go/install.go:1737 ux.Info` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[i]` | `os.Stdout, "Skill subset: %s", strings.Join(skillSubset, ", "))` | match |
| `cmd/apm-go/install.go:1745 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/install.go:1777 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "%s deployed 0 files to any target", dep.Key)` | match |
| `cmd/apm-go/install.go:1854 ux.Info` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[i]` | `os.Stdout, "MCP servers configured:")` | match |
| `cmd/apm-go/install.go:1855 ux.BulletList` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `(none;  - continuation)` | `os.Stdout, mcpItems)` | match |
| `cmd/apm-go/install.go:1868 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/install.go:1884 ux.Info` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[i]` | `os.Stdout, "Already up to date")` | match |
| `cmd/apm-go/install.go:1939 ux.Success` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[*]` | `os.Stdout, "Installed %s", strings.Join(summaryParts, " and "))` | match |
| `cmd/apm-go/install.go:1969 ux.BulletList` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `(none;  - continuation)` | `os.Stdout, items)` | match |
| `cmd/apm-go/install.go:2586 ux.Warn` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[!]` | `os.Stderr, "%s", w)` | match |
| `cmd/apm-go/install.go:2685 ux.Info` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[i]` | `os.Stdout, "%s: moved from dependencies.apm to devDependencies.apm (--dev)", key)` | match |
| `cmd/apm-go/install.go:2694 ux.Info` | `commands/install.py:568-688, 1424-1625; core/command_logger.py:789-865 (install)` | `[i]` | `os.Stdout, "%s: moved from devDependencies.apm to dependencies.apm", key)` | match |
| `cmd/apm-go/main.go:79 ux.Error` | `core/command_logger.py:132-140 (diagnostics)` | `[x]` | `os.Stderr, "%s", err)` | match |
| `cmd/apm-go/main.go:127 ux.Warn` | `core/command_logger.py:132-140 (diagnostics)` | `[!]` | `os.Stderr, "%d diagnostic(s) found in %s", len(diags), args[0])` | match |
| `cmd/apm-go/main.go:132 ux.BulletList` | `core/command_logger.py:132-140 (diagnostics)` | `(none;  - continuation)` | `os.Stderr, items)` | match |
| `cmd/apm-go/marketplace.go:214 ux.Info` | `commands/marketplace/__init__.py:632 (start, gear)` | `[i]` | `w, "Registering marketplace %q...", provisionalLabel)` | match |
| `cmd/apm-go/marketplace.go:218 ux.Warn` | `commands/marketplace/validate.py:29-90 (validate)` | `[!]` | `cmd.ErrOrStderr(), "Pin this git marketplace with a #ref (e.g. SOURCE#v1.2.3) to avoid silently tracking a moving branch")` | match |
| `cmd/apm-go/marketplace.go:228 ux.Warn` | `commands/marketplace/validate.py:29-90 (validate)` | `[!]` | `cmd.ErrOrStderr(), "%s", aliasWarning)` | match |
| `cmd/apm-go/marketplace.go:238 ux.Success` | `commands/marketplace/__init__.py:718-721 (success, check)` | `[*]` | `w, "Marketplace %q registered (%d plugins)", effectiveName, len(m.Plugins))` | match |
| `cmd/apm-go/marketplace.go:250 ux.BulletList` | `commands/marketplace/validate.py:29-90 (validate)` | `(none;  - continuation)` | `w, items)` | match |
| `cmd/apm-go/marketplace.go:256 ux.Info` | `commands/marketplace/validate.py:29-90 (validate)` | `[i]` | `w, "Install plugins with: apm-go install <plugin>@%s", effectiveName)` | match |
| `cmd/apm-go/marketplace.go:406 ux.Info` | `commands/marketplace/__init__.py:856-890 (list)` | `[i]` | `w, "No marketplaces registered. Use 'apm-go marketplace add SOURCE' to register one (OWNER/REPO, HTTPS URL, SSH URL, or local path).")` | match |
| `cmd/apm-go/marketplace.go:441 ux.Info` | `commands/marketplace/__init__.py:856-890 (list)` | `[i]` | `w, "Use 'apm-go marketplace browse <name>' to see plugins")` | match |
| `cmd/apm-go/marketplace.go:479 ux.Warn` | `commands/marketplace/__init__.py:912-927 (browse)` | `[!]` | `w, "Marketplace '%s' has no plugins", name)` | match |
| `cmd/apm-go/marketplace.go:497 ux.BulletList` | `commands/marketplace/__init__.py:912-927 (browse)` | `(none;  - continuation)` | `w, []ux.Item{{Text: fmt.Sprintf("%d plugin(s) in %q", len(m.Plugins), name)}})` | match |
| `cmd/apm-go/marketplace.go:499 ux.Info` | `commands/marketplace/__init__.py:912-927 (browse)` | `[i]` | `w, "Install a plugin: apm-go install <plugin-name>@%s", name)` | match |
| `cmd/apm-go/marketplace.go:532 ux.Info` | `commands/marketplace/__init__.py:977-999 (update)` | `[i]` | `w, "Refreshing marketplace %q...", name)` | match |
| `cmd/apm-go/marketplace.go:537 ux.Success` | `commands/marketplace/__init__.py:977-999 (update)` | `[*]` | `w, "Refreshed marketplace %q (%d plugins)", name, len(m.Plugins))` | match |
| `cmd/apm-go/marketplace.go:539 ux.BulletList` | `commands/marketplace/__init__.py:977-999 (update)` | `(none;  - continuation)` | `w, []ux.Item{{Text: fmt.Sprintf("source: %s", displaySource(src))}})` | match |
| `cmd/apm-go/marketplace.go:551 ux.Info` | `commands/marketplace/__init__.py:977-999 (update)` | `[i]` | `w, "No marketplaces registered.")` | match |
| `cmd/apm-go/marketplace.go:554 ux.Info` | `commands/marketplace/__init__.py:977-999 (update)` | `[i]` | `w, "Refreshing %d marketplace(s)...", len(sources))` | match |
| `cmd/apm-go/marketplace.go:559 ux.Error` | `commands/marketplace/__init__.py:977-999 (update)` | `[x]` | `cmd.ErrOrStderr(), "failed to refresh marketplace %q: %v", s.Name, ferr)` | match |
| `cmd/apm-go/marketplace.go:562 ux.Success` | `commands/marketplace/__init__.py:977-999 (update)` | `[*]` | `w, "Refreshed marketplace %q (%d plugins)", s.Name, len(m.Plugins))` | match |
| `cmd/apm-go/marketplace.go:564 ux.BulletList` | `commands/marketplace/__init__.py:977-999 (update)` | `(none;  - continuation)` | `w, []ux.Item{{Text: fmt.Sprintf("source: %s", displaySource(s))}})` | match |
| `cmd/apm-go/marketplace.go:568 ux.Success` | `commands/marketplace/__init__.py:977-999 (update)` | `[*]` | `w, "Marketplace cache refreshed")` | match |
| `cmd/apm-go/marketplace.go:618 ux.Info` | `commands/marketplace/__init__.py:1024-1039 (remove)` | `[i]` | `cmd.ErrOrStderr(), "Cancelled")` | match |
| `cmd/apm-go/marketplace.go:625 ux.Success` | `commands/marketplace/__init__.py:1024-1039 (remove)` | `[*]` | `cmd.OutOrStdout(), "Removed marketplace %q", name)` | match |
| `cmd/apm-go/marketplace.go:627 ux.BulletList` | `commands/marketplace/__init__.py:1024-1039 (remove)` | `(none;  - continuation)` | `cmd.OutOrStdout(), []ux.Item{{Text: fmt.Sprintf("source: %s", displaySource(src))}})` | match |
| `cmd/apm-go/marketplace.go:672 ux.Gear` | `commands/marketplace/validate.py:29-90 (validate)` | `[*]` | `w, "Validating marketplace '%s'...", name)` | match |
| `cmd/apm-go/marketplace.go:702 ux.Info` | `commands/marketplace/validate.py:29-90 (validate)` | `[i]` | `w, "Found %d plugins", len(m.Plugins))` | match |
| `cmd/apm-go/marketplace.go:717 ux.BulletList` | `commands/marketplace/validate.py:29-90 (validate)` | `(none;  - continuation)` | `w, items)` | match |
| `cmd/apm-go/marketplace.go:725 ux.Warn` | `commands/marketplace/validate.py:29-90 (validate)` | `[!]` | `w, "Ref checking not yet implemented -- skipping ref reachability checks")` | match |
| `cmd/apm-go/marketplace.go:730 ux.Info` | `commands/marketplace/validate.py:29-90 (validate)` | `[i]` | `w, "Validation Results:")` | match |
| `cmd/apm-go/marketplace.go:752 ux.Check` | `commands/marketplace/validate.py:29-90 (validate)` | `[+]` | `w, "  %s: passed", check.CheckName)` | match |
| `cmd/apm-go/marketplace.go:756 ux.Warn` | `commands/marketplace/validate.py:29-90 (validate)` | `[!]` | `w, "  %s: %s", check.CheckName, f.Message)` | match |
| `cmd/apm-go/marketplace.go:766 ux.Error` | `commands/marketplace/validate.py:29-90 (validate)` | `[x]` | `w, "  %s: %s", check.CheckName, f.Message)` | match |
| `cmd/apm-go/marketplace.go:772 ux.Warn` | `commands/marketplace/validate.py:29-90 (validate)` | `[!]` | `w, "  %s: %s", check.CheckName, f.Message)` | match |
| `cmd/apm-go/marketplace.go:779 ux.Info` | `commands/marketplace/validate.py:29-90 (validate)` | `[i]` | `w, "Summary: %d passed, %d warnings, %d errors", passed, warnings, errs)` | match |
| `cmd/apm-go/marketplace_authoring.go:95 ux.Success` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `[*]` | `w, "Created apm.yml with 'marketplace:' block")` | match |
| `cmd/apm-go/marketplace_authoring.go:97 ux.Success` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `[*]` | `w, "Added 'marketplace:' block to apm.yml")` | match |
| `cmd/apm-go/marketplace_authoring.go:102 ux.BulletList` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `(none;  - continuation)` | `w, []ux.Item{{Text: fmt.Sprintf("Path: %s", filepath.Join(cwd, "apm.yml"))}})` | match |
| `cmd/apm-go/marketplace_authoring.go:276 ux.Warn` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `[!]` | `cmd.ErrOrStderr(), "reading legacy marketplace.yml; run 'apm-go marketplace migrate' to fold it into apm.yml")` | match |
| `cmd/apm-go/marketplace_authoring.go:285 ux.Warn` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `[!]` | `cmd.ErrOrStderr(), "%s", w)` | match |
| `cmd/apm-go/marketplace_authoring.go:291 ux.Info` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `[i]` | `w, "Offline mode -- only schema and cached-ref checks")` | match |
| `cmd/apm-go/marketplace_authoring.go:316 ux.Success` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `[*]` | `w, "all %d package(s) verified", len(results))` | match |
| `cmd/apm-go/marketplace_authoring.go:361 ux.Warn` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `[!]` | `cmd.ErrOrStderr(), "reading legacy marketplace.yml; run 'apm-go marketplace migrate' to fold it into apm.yml")` | match |
| `cmd/apm-go/marketplace_authoring.go:384 ux.Info` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `[i]` | `w, "%d package(s) can be updated", upgradable)` | match |
| `cmd/apm-go/marketplace_authoring.go:386 ux.Info` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `[i]` | `w, "All packages are up to date")` | match |
| `cmd/apm-go/marketplace_authoring.go:389 ux.BulletList` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `(none;  - continuation)` | `w, []ux.Item{{Text: fmt.Sprintf("%d upgradable entries", upgradable)}})` | match |
| `cmd/apm-go/marketplace_authoring.go:474 ux.Warn` | `commands/marketplace/init.py:97-99; audit.py:62-107; outdated.py (authoring)` | `[!]` | `w, "Your .gitignore ignores marketplace.json. Track apm.yml plus generated "+` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:59 ux.Error` | `commands/marketplace/audit.py:40-107 (audit)` | `[x]` | `w, "%s", err)` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:60 ux.Info` | `commands/marketplace/audit.py:40-107 (audit)` | `[i]` | `w, "Run with --verbose for details.")` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:88 ux.Info` | `commands/marketplace/audit.py:40-107 (audit)` | `[i]` | `w, "Auditing marketplace %q...", name)` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:97 ux.Info` | `commands/marketplace/audit.py:40-107 (audit)` | `[i]` | `w, "Checking %d plugin%s...", len(m.Plugins), plural)` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:123 ux.Info` | `commands/marketplace/audit.py:40-107 (audit)` | `[i]` | `w, "Audit Results:")` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:134 ux.Success` | `commands/marketplace/audit.py:40-107 (audit)` | `[*]` | `w, "%s", summary)` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:136 ux.Info` | `commands/marketplace/audit.py:40-107 (audit)` | `[i]` | `w, "%s", summary)` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:141 ux.Info` | `commands/marketplace/audit.py:40-107 (audit)` | `[i]` | `w, "Marketplace refs (name@marketplace) pin transitive deps through the catalogue "+` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:151 ux.Info` | `commands/marketplace/audit.py:40-107 (audit)` | `[i]` | `w, "Run 'apm-go marketplace audit %s --strict --verbose' to see skipped plugin reasons.", name)` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:157 ux.Info` | `commands/marketplace/audit.py:40-107 (audit)` | `[i]` | `w, "Run 'apm-go marketplace audit %s --strict --verbose' to see skipped plugin reasons.", name)` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:178 ux.Success` | `commands/marketplace/audit.py:40-107 (audit)` | `[*]` | `w, "%s: deps are marketplace-resolved", r.PluginName)` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:187 ux.Info` | `commands/marketplace/audit.py:40-107 (audit)` | `[i]` | `w, "%s: skipped (%s)", r.PluginName, r.Detail)` | match |
| `cmd/apm-go/marketplace_authoring_audit.go:191 ux.Warn` | `commands/marketplace/audit.py:40-107 (audit)` | `[!]` | `w, "%s: could not verify (%s)", r.PluginName, r.Detail)` | match |
| `cmd/apm-go/marketplace_authoring_migrate.go:33 ux.Section` | `commands/marketplace/migrate.py:55-62 (migrate)` | `(none)` | `w, "Dry run -- the following changes would be applied to apm.yml:")` | match |
| `cmd/apm-go/marketplace_authoring_migrate.go:35 ux.Info` | `commands/marketplace/migrate.py:55-62 (migrate)` | `[i]` | `w, "(no changes)")` | match |
| `cmd/apm-go/marketplace_authoring_migrate.go:42 ux.Success` | `commands/marketplace/migrate.py:55-62 (migrate)` | `[*]` | `w, "Migrated marketplace.yml into apm.yml's 'marketplace:' block")` | match |
| `cmd/apm-go/marketplace_authoring_migrate.go:43 ux.Info` | `commands/marketplace/migrate.py:55-62 (migrate)` | `[i]` | `w, "marketplace.yml has been removed. Commit apm.yml to record the migration.")` | match |
| `cmd/apm-go/marketplace_package.go:187 ux.Warn` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[!]` | `cmd.ErrOrStderr(), "'HEAD' is a mutable ref. Resolving to current SHA for safety.")` | match |
| `cmd/apm-go/marketplace_package.go:193 ux.Info` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[i]` | `cmd.OutOrStdout(), "Resolved %s to %s", ref, shortSHA(sha))` | match |
| `cmd/apm-go/marketplace_package.go:201 ux.Warn` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[!]` | `cmd.ErrOrStderr(), "packages: block structure required rewriting the whole list; hand formatting on other entries may have changed")` | match |
| `cmd/apm-go/marketplace_package.go:204 ux.Warn` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[!]` | `cmd.ErrOrStderr(), "package %q has no --category; marketplace.outputs includes 'codex', which requires one at `pack` time", resolved)` | match |
| `cmd/apm-go/marketplace_package.go:206 ux.Success` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[*]` | `cmd.OutOrStdout(), "Added package %q from %s", resolved, args[0])` | match |
| `cmd/apm-go/marketplace_package.go:295 ux.Warn` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[!]` | `cmd.ErrOrStderr(), "'HEAD' is a mutable ref. Resolving to current SHA for safety.")` | match |
| `cmd/apm-go/marketplace_package.go:301 ux.Info` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[i]` | `cmd.OutOrStdout(), "Resolved %s to %s", ref, shortSHA(sha))` | match |
| `cmd/apm-go/marketplace_package.go:309 ux.Warn` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[!]` | `cmd.ErrOrStderr(), "packages: block structure required rewriting the whole list; hand formatting on other entries may have changed")` | match |
| `cmd/apm-go/marketplace_package.go:311 ux.Success` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[*]` | `cmd.OutOrStdout(), "Updated package %q", args[0])` | match |
| `cmd/apm-go/marketplace_package.go:361 ux.Info` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[i]` | `cmd.ErrOrStderr(), "Cancelled")` | match |
| `cmd/apm-go/marketplace_package.go:368 ux.Success` | `commands/marketplace/plugin/add.py:93; set.py:111; remove.py:52 (package)` | `[*]` | `cmd.OutOrStdout(), "Removed package %q", name)` | match |
| `cmd/apm-go/marketplace_resolve.go:47 ux.Warn` | `apm-go-only (no Oracle callsite for this helper)` | `[!]` | `os.Stderr, "%s", w)` | apm-go-only |
| `cmd/apm-go/mcp_prompt.go:89 ux.Section` | `commands/install/mcp/command.py:111-241 (MCP install)` | `(none)` | `os.Stderr, "Credentials needed")` | match |
| `cmd/apm-go/mcp_prompt.go:121 ux.Warn` | `commands/install/mcp/command.py:111-241 (MCP install)` | `[!]` | `os.Stderr, "MCP server %q already exists. Replacement diff:", name)` | match |
| `cmd/apm-go/mcp_prompt.go:126 ux.BulletList` | `commands/install/mcp/command.py:111-241 (MCP install)` | `(none;  - continuation)` | `os.Stderr, items)` | match |
| `cmd/apm-go/mcpinstall.go:96 ux.Info` | `commands/install/mcp/command.py:111-241 (MCP install)` | `[i]` | `os.Stdout, "MCP server %q unchanged", opts.Name)` | match |
| `cmd/apm-go/mcpinstall.go:109 ux.Warn` | `commands/install/mcp/command.py:111-241 (MCP install)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/mcpinstall.go:137 ux.Warn` | `commands/install/mcp/command.py:111-241 (MCP install)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/mcpinstall.go:146 ux.Info` | `commands/install/mcp/command.py:111-241 (MCP install)` | `[i]` | `os.Stdout, "Targets: %s  (source: %s)", strings.Join(targets, ", "), targetSource)` | match |
| `cmd/apm-go/mcpinstall.go:159 ux.Info` | `commands/install/mcp/command.py:111-241 (MCP install)` | `[i]` | `os.Stdout, "Skipped MCP config for %s  (active targets: %s)",` | match |
| `cmd/apm-go/mcpinstall.go:168 ux.Warn` | `commands/install/mcp/command.py:111-241 (MCP install)` | `[!]` | `os.Stdout, "MCP server %q declared in apm.yml but not deployed to any target; see diagnostics above", opts.Name)` | match |
| `cmd/apm-go/mcpinstall.go:171 ux.Success` | `commands/install/mcp/command.py:111-241 (MCP install)` | `[*]` | `os.Stdout, "%s MCP server %q", verb, opts.Name)` | match |
| `cmd/apm-go/mcpinstall.go:184 ux.BulletList` | `commands/install/mcp/command.py:111-241 (MCP install)` | `(none;  - continuation)` | `os.Stdout, []ux.Item{` | match |
| `cmd/apm-go/mcpinstall.go:635 ux.Warn` | `commands/install/mcp/command.py:111-241 (MCP install)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/mcpinstall.go:651 ux.Warn` | `commands/install/mcp/command.py:111-241 (MCP install)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/pack.go:717 ux.Section` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `(none)` | `w, fmt.Sprintf("dry-run: Would pack %d file(s) -> %s", len(result.Files), result.BundleDir))` | match |
| `cmd/apm-go/pack.go:722 ux.BulletList` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `(none;  - continuation)` | `w, items)` | match |
| `cmd/apm-go/pack.go:726 ux.Sparkle` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[*]` | `w, "Packed %d file(s) -> %s%s", len(result.Files), displayDir, bundleSizeSuffix(result.BundleDir))` | match |
| `cmd/apm-go/pack.go:741 ux.BulletList` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `(none;  - continuation)` | `w, items)` | match |
| `cmd/apm-go/pack.go:751 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "Note: --archive now produces .zip by default. Use --archive-format tar.gz "+` | match |
| `cmd/apm-go/pack.go:762 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "Claude plugin bundle ready -- contains plugin.json plus "+` | match |
| `cmd/apm-go/pack.go:764 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "Share with: apm-go install %s", displayDir)` | match |
| `cmd/apm-go/pack.go:913 ux.Warn` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[!]` | `cmd.ErrOrStderr(), "reading legacy marketplace.yml; run 'apm-go marketplace migrate' to fold it into apm.yml")` | match |
| `cmd/apm-go/pack.go:940 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "No marketplace outputs selected; nothing to write.")` | match |
| `cmd/apm-go/pack.go:953 ux.Warn` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[!]` | `cmd.ErrOrStderr(), "%s", warning)` | match |
| `cmd/apm-go/pack.go:960 ux.BulletList` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `(none;  - continuation)` | `w, items)` | match |
| `cmd/apm-go/pack.go:1004 ux.Warn` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[!]` | `cmd.ErrOrStderr(), "%s", warning)` | match |
| `cmd/apm-go/pack.go:1037 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "dry-run: Would write marketplace.json [%s] (%d package(s)) -> %s", r.format, r.count, r.absPath)` | match |
| `cmd/apm-go/pack.go:1040 ux.Sparkle` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[*]` | `w, "Built marketplace.json [%s] (%d package(s)) -> %s", r.format, r.count, r.absPath)` | match |
| `cmd/apm-go/pack.go:1076 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "Marketplace artifacts ready:")` | match |
| `cmd/apm-go/pack.go:1078 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "  [%-*s] %s", labelWidth, r.format, r.absPath)` | match |
| `cmd/apm-go/pack.go:1080 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "How consumers install from this marketplace varies by AI assistant.")` | match |
| `cmd/apm-go/pack.go:1081 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "See: %s", marketplaceDocsURL)` | match |
| `cmd/apm-go/pack.go:1178 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "Version alignment check skipped: no marketplace block; nothing to check.")` | match |
| `cmd/apm-go/pack.go:1181 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "Marketplace drift check skipped: no marketplace block; nothing to check.")` | match |
| `cmd/apm-go/pack.go:1216 ux.Warn` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[!]` | `cmd.ErrOrStderr(), "%s", warning)` | match |
| `cmd/apm-go/pack.go:1241 ux.Sparkle` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[*]` | `w, "Version alignment OK [strategy=%s, expected=%s]", report.Strategy, report.Expected)` | match |
| `cmd/apm-go/pack.go:1243 ux.Sparkle` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[*]` | `w, "Version alignment OK [strategy=%s]", report.Strategy)` | match |
| `cmd/apm-go/pack.go:1246 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    %s  %s%s  [%s]", row.Path, row.Version, tagSuffix(row.RenderedTag), row.Reason)` | match |
| `cmd/apm-go/pack.go:1252 ux.Error` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[x]` | `w, "Version alignment failed [strategy=%s, expected=%s]", report.Strategy, report.Expected)` | match |
| `cmd/apm-go/pack.go:1254 ux.Error` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[x]` | `w, "Version alignment failed [strategy=%s]", report.Strategy)` | match |
| `cmd/apm-go/pack.go:1261 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    %s  %s%s  [%s]", row.Path, versionStr, tagSuffix(row.RenderedTag), row.Reason)` | match |
| `cmd/apm-go/pack.go:1264 ux.Error` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[x]` | `w, "    %s", msg)` | match |
| `cmd/apm-go/pack.go:1288 ux.Sparkle` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[*]` | `w, "Marketplace working tree clean [outputs=%s]", strings.Join(formats, ", "))` | match |
| `cmd/apm-go/pack.go:1290 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    %s  [unchanged]", out.Path)` | match |
| `cmd/apm-go/pack.go:1301 ux.Error` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[x]` | `w, "Marketplace working tree dirty [outputs=%s]", strings.Join(dirtyFormats, ", "))` | match |
| `cmd/apm-go/pack.go:1305 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    %s  [unchanged]", out.Path)` | match |
| `cmd/apm-go/pack.go:1307 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    %s  [missing on disk; would be created]", out.Path)` | match |
| `cmd/apm-go/pack.go:1310 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    %s  [drift: %d differences]", out.Path, len(out.Differences))` | match |
| `cmd/apm-go/pack.go:1312 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "%s", line)` | match |
| `cmd/apm-go/pack.go:1332 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "")` | match |
| `cmd/apm-go/pack.go:1333 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    To recover cleanly (fold into the current commit):")` | match |
| `cmd/apm-go/pack.go:1334 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "")` | match |
| `cmd/apm-go/pack.go:1335 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "      %s                       # regenerate locally", packCommand)` | match |
| `cmd/apm-go/pack.go:1336 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "      %s", stageCommand)` | match |
| `cmd/apm-go/pack.go:1337 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "      git commit --amend --no-edit   # fold into the current commit")` | match |
| `cmd/apm-go/pack.go:1338 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "      git push --force-with-lease    # safe re-push")` | match |
| `cmd/apm-go/pack.go:1339 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "")` | match |
| `cmd/apm-go/pack.go:1340 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    Or as a follow-up commit:")` | match |
| `cmd/apm-go/pack.go:1341 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "")` | match |
| `cmd/apm-go/pack.go:1342 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "      %s && %s", packCommand, stageCommand)` | match |
| `cmd/apm-go/pack.go:1343 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "      git commit -m 'chore(marketplace): regen'")` | match |
| `cmd/apm-go/pack.go:1344 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "")` | match |
| `cmd/apm-go/pack.go:1345 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    Why this exists: marketplace.json is checked in (lockfile pattern)")` | match |
| `cmd/apm-go/pack.go:1346 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    so consumers can resolve packages without running 'apm pack'. CI")` | match |
| `cmd/apm-go/pack.go:1347 ux.Info` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[i]` | `w, "    enforces that the checked-in copy matches the apm.yml source of truth.")` | match |
| `cmd/apm-go/pack.go:1428 ux.Warn` | `commands/pack.py:668-704, 735-777, 445-519 (pack)` | `[!]` | `w, "%s", licenseUndeclaredWarning)` | match |
| `cmd/apm-go/search.go:106 ux.Running` | `commands/marketplace/__init__.py:1389-1406 (search)` | `[>]` | `w, "Searching '%s' for '%s'...", marketplaceName, query)` | match |
| `cmd/apm-go/search.go:112 ux.BulletList` | `commands/marketplace/__init__.py:1389-1406 (search)` | `(none;  - continuation)` | `cmd.ErrOrStderr(), []ux.Item{{Text: err.Error()}})` | match |
| `cmd/apm-go/search.go:132 ux.Warn` | `commands/marketplace/__init__.py:1389-1406 (search)` | `[!]` | `w,` | match |
| `cmd/apm-go/search.go:153 ux.Info` | `commands/marketplace/__init__.py:1389-1406 (search)` | `[i]` | `w, "Install: apm-go install <plugin-name>@%s", marketplaceName)` | match |
| `cmd/apm-go/uninstall.go:77 ux.Error` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[x]` | `os.Stderr, "%q: refused to remove -- registry resolved %q, which is not present in apm.lock.yaml (supply-chain guard)", rej.Name, rej.Canonical)` | match |
| `cmd/apm-go/uninstall.go:81 ux.Info` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[i]` | `os.Stdout, "No packages found in apm.yml to remove")` | match |
| `cmd/apm-go/uninstall.go:249 ux.Warn` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/uninstall.go:256 ux.BulletList` | `commands/uninstall/cli.py:290-494 (uninstall)` | `(none;  - continuation)` | `os.Stdout, items)` | match |
| `cmd/apm-go/uninstall.go:281 ux.Warn` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/uninstall.go:504 ux.BulletList` | `commands/uninstall/cli.py:290-494 (uninstall)` | `(none;  - continuation)` | `os.Stdout, removed)` | match |
| `cmd/apm-go/uninstall.go:522 ux.Warn` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[!]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/uninstall.go:566 ux.Warn` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[!]` | `os.Stderr, "%q: cannot preview with --dry-run (marketplace reference has no lockfile anchor); use owner/repo, or run without --dry-run", nf.Name)` | match |
| `cmd/apm-go/uninstall.go:569 ux.Warn` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[!]` | `os.Stderr, "%q: could not resolve (%s)", nf.Name, nf.Detail)` | match |
| `cmd/apm-go/uninstall.go:571 ux.Warn` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[!]` | `os.Stderr, "%q: could not resolve", nf.Name)` | match |
| `cmd/apm-go/uninstall.go:574 ux.Warn` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[!]` | `os.Stderr, "%q: not found in apm.yml", nf.Name)` | match |
| `cmd/apm-go/uninstall.go:582 ux.Section` | `commands/uninstall/cli.py:290-494 (uninstall)` | `(none)` | `os.Stdout, "dry-run: would remove from apm.yml")` | match |
| `cmd/apm-go/uninstall.go:594 ux.BulletList` | `commands/uninstall/cli.py:290-494 (uninstall)` | `(none;  - continuation)` | `os.Stdout, targetItems)` | match |
| `cmd/apm-go/uninstall.go:597 ux.Section` | `commands/uninstall/cli.py:290-494 (uninstall)` | `(none)` | `os.Stdout, "dry-run: transitive orphans that would also be removed")` | match |
| `cmd/apm-go/uninstall.go:602 ux.BulletList` | `commands/uninstall/cli.py:290-494 (uninstall)` | `(none;  - continuation)` | `os.Stdout, orphanItems)` | match |
| `cmd/apm-go/uninstall.go:605 ux.Section` | `commands/uninstall/cli.py:290-494 (uninstall)` | `(none)` | `os.Stdout, "dry-run: apm_modules")` | match |
| `cmd/apm-go/uninstall.go:614 ux.BulletList` | `commands/uninstall/cli.py:290-494 (uninstall)` | `(none;  - continuation)` | `os.Stdout, moduleItems)` | match |
| `cmd/apm-go/uninstall.go:616 ux.Info` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[i]` | `os.Stdout, "dry-run: no changes made")` | match |
| `cmd/apm-go/uninstall.go:633 ux.Success` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[*]` | `os.Stdout, "Removed %d package(s) (+%d transitive orphan(s))", removedPackages, len(orphans))` | match |
| `cmd/apm-go/uninstall.go:635 ux.Success` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[*]` | `os.Stdout, "Removed %d package(s)", removedPackages)` | match |
| `cmd/apm-go/uninstall.go:645 ux.BulletList` | `commands/uninstall/cli.py:290-494 (uninstall)` | `(none;  - continuation)` | `os.Stdout, items)` | match |
| `cmd/apm-go/uninstall.go:649 ux.Info` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[i]` | `os.Stdout, "apm.yml updated: %s", apmYMLPath)` | match |
| `cmd/apm-go/uninstall.go:652 ux.Success` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[*]` | `os.Stdout, "apm_modules: removed %d director%s", removedModuleDirs, pluralYIES(removedModuleDirs))` | match |
| `cmd/apm-go/uninstall.go:656 ux.Success` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[*]` | `os.Stdout, "cleaned %d integrated file(s) (%d kept -- modified or shared)", len(removedFiles), len(keptFiles))` | match |
| `cmd/apm-go/uninstall.go:658 ux.Success` | `commands/uninstall/cli.py:290-494 (uninstall)` | `[*]` | `os.Stdout, "cleaned %d integrated file(s)", len(removedFiles))` | match |
| `cmd/apm-go/update.go:231 ux.Error` | `commands/update.py:17-20; install/plan.py:333 (update plan)` | `[x]` | `os.Stderr, "%s", d)` | match |
| `cmd/apm-go/update.go:360 ux.Section` | `commands/update.py:17-20; install/plan.py:333 (update plan)` | `(none)` | `os.Stdout, "Update plan for apm.yml")` | match |
| `cmd/apm-go/update.go:361 ux.BulletList` | `commands/update.py:17-20; install/plan.py:333 (update plan)` | `(none;  - continuation)` | `os.Stdout, items)` | match |
| `internal/pack/bundle/producer.go:171 ux.Warn` | `commands/pack.py:370-377, 431-432, 646-677 (pack producer)` | `[!]` | `w, "%s", c)` | match |
| `internal/pack/bundle/producer.go:291 ux.Warn` | `commands/pack.py:370-377, 431-432, 646-677 (pack producer)` | `[!]` | `w, "Secrets withheld from plugin.json so they are never committed as "+` | match |
| `internal/pack/bundle/producer.go:393 ux.Warn` | `commands/pack.py:370-377, 431-432, 646-677 (pack producer)` | `[!]` | `w, "Bundle contains %d hidden character(s) across source files "+` | match |
| `internal/pack/bundle/producer.go:464 ux.Warn` | `commands/pack.py:370-377, 431-432, 646-677 (pack producer)` | `[!]` | `w, "Found plugin.json at %s but could not parse it: %v. Falling back to synthesis from apm.yml.", p, perr)` | match |
| `internal/pack/bundle/producer.go:470 ux.Info` | `commands/pack.py:370-377, 431-432, 646-677 (pack producer)` | `[i]` | `w, "No plugin.json found; synthesising from apm.yml.")` | match |
| `internal/pack/bundle/producer.go:503 ux.Warn` | `commands/pack.py:370-377, 431-432, 646-677 (pack producer)` | `[!]` | `w, "Stripped schema-invalid keys from authored plugin.json: %s "+` | match |
| `internal/pack/pluginmanifest/write.go:44 ux.Warn` | `core/plugin_manifest.py:483-484 (manifest write)` | `[!]` | `w, "unknown plugin ecosystem %q; skipping plugin.json generation.", ecosystem)` | match |
| `internal/pack/pluginmanifest/write.go:54 ux.Info` | `core/plugin_manifest.py:483-484 (manifest write)` | `[i]` | `w, "Would write plugin manifest to %s", absPath)` | match |
| `internal/pack/pluginmanifest/write.go:60 ux.Warn` | `core/plugin_manifest.py:483-484 (manifest write)` | `[!]` | `w, "%s already exists; skipping plugin.json generation. Re-run with --force to overwrite it.", absPath)` | match |
| `internal/pack/pluginmanifest/write.go:63 ux.Warn` | `core/plugin_manifest.py:483-484 (manifest write)` | `[!]` | `w, "Overwriting %s with generated manifest from apm.yml (--force).", absPath)` | match |
| `internal/pack/pluginmanifest/write.go:67 ux.Info` | `core/plugin_manifest.py:483-484 (manifest write)` | `[i]` | `w, "Writing generated plugin manifest under .github/: %s", absPath)` | match |
| `internal/pack/pluginmanifest/write.go:84 ux.Success` | `core/plugin_manifest.py:483-484 (manifest write)` | `[*]` | `w, "Generated plugin manifest: %s", absPath)` | match |

## Reversed by ticket 33 (user ruling)

The glyph-shape half of this audit is reversed by the 2026-08-29 user ruling:
apm-go must emit centered width-3 project TUI symbols, and the parity runner
must normalize those symbols against the Oracle's bracket forms. The stream
channel ruling remains unchanged: errors and warnings are stdout records.
