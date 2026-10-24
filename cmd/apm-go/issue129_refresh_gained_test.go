package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A path the new copy of the package that stays gains, and that another
// package that stays (or the project's own content) records, stays that
// package's also when the user removed its file before the uninstall:
// nothing is written there, the lock keeps its record alone, and a frozen
// install restores its file. Oracle 0.33.0 writes the file from ./b/dep and
// records the path under both packages with C's hash, and its --frozen, an
// ordinary install, passes (review run r4/p2-cgainmiss-or); apm-go's frozen
// install would fail on that lock, which PRODUCT.md rules out. A copilot hook
// of a package is <package>-<stem>.json, so two packages' hooks share a path
// only when the project's own content is named like ./b/dep's directory, in a
// project (at user scope the project's own hooks are local-<stem>.json).
func TestUninstall_SharedLocalSlot_GainedPathOfAnotherPackageRemoved(t *testing.T) {
	type gained struct {
		source, body, target string
		dest                 func(global bool) string
	}
	kinds := map[string]gained{
		"skill":  {otherSource("skill", "sy"), otherBody("skill", "sy", "%s"), "claude", func(bool) string { return ".claude/skills/sy/SKILL.md" }},
		"agent":  {otherSource("agent", "ry"), otherBody("agent", "ry", "%s"), "claude", func(bool) string { return ".claude/agents/ry.md" }},
		"instr":  {otherSource("instr", "iy"), otherBody("instr", "iy", "%s"), "claude", func(bool) string { return ".claude/rules/iy.md" }},
		"cmd":    {otherSource("cmd", "cy"), otherBody("cmd", "cy", "%s"), "claude", func(bool) string { return ".claude/commands/cy.md" }},
		"prompt": {otherSource("cmd", "cy"), otherBody("cmd", "cy", "%s"), "copilot", copilotPromptDest},
		"hook":   {".apm/hooks/h.json", `{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"echo %s"}]}]}}` + "\n", "copilot", func(bool) string { return ".github/hooks/dep-h.json" }},
	}
	for _, global := range []bool{false, true} {
		for _, order := range []string{"cfirst", "clast", "local"} {
			for _, kind := range []string{"skill", "agent", "instr", "cmd", "prompt", "hook"} {
				if kind == "hook" && (global || order != "local") {
					continue
				}
				t.Run(scopeName(global)+"/"+order+"/"+kind, func(t *testing.T) {
					g := kinds[kind]
					body := func(who string) string { return strings.ReplaceAll(g.body, "%s", who) }
					project := "proj"
					if kind == "hook" {
						project = "dep"
					}
					s := newGainedState(t, global, order == "local", order == "cfirst", g.target, project, g.source, body("c"))
					dest := g.dest(global)
					full := filepath.Join(s.deployRoot, filepath.FromSlash(dest))
					_, _, cFiles, cHashes := s.lock(t)
					cHash := cHashes[dest]
					if cHash == "" || !hasLine(cFiles, dest) {
						t.Fatalf("setup: C does not record %s: %v", dest, cFiles)
					}
					writeCompileFile(t, filepath.Join(s.srcRoot, "b", "dep"), g.source, body("b"))
					if kind == "skill" {
						mustRemoveAll(t, filepath.Join(s.deployRoot, ".claude", "skills", "sy"))
					} else {
						mustRemove(t, full)
					}
					s.run(t, "uninstall", s.a)
					// copilot shares .agents/skills with targets the project
					// does not deploy to, and nothing is deployed for them.
					for _, d := range []string{".codex", ".opencode", ".config/opencode", ".agents/skills/sy"} {
						if _, err := os.Lstat(filepath.Join(s.deployRoot, filepath.FromSlash(d))); !os.IsNotExist(err) {
							t.Errorf("uninstall made %s (%v)", d, err)
						}
					}
					text, b, cFiles, cHashes := s.lock(t)
					bRecords := slices.ContainsFunc(b.DeployedFiles, func(f string) bool { return f == dest || kind == "skill" && strings.HasPrefix(f, ".claude/skills/sy") })
					cRecords := slices.ContainsFunc(cFiles, func(f string) bool { return f == dest || kind == "skill" && strings.HasPrefix(f, ".claude/skills/sy") })
					want := cHash
					if order == "clast" {
						// ./b/dep is declared first, so restore writes its file,
						// which the lock records under it alone.
						data, err := os.ReadFile(full)
						if err != nil {
							t.Fatalf("uninstall did not write %s: %v", dest, err)
						}
						sum := sha256.Sum256(data)
						want = "sha256:" + hex.EncodeToString(sum[:])
						if want == cHash || b.DeployedHashes[dest] != want || cRecords {
							t.Errorf("%s = %q, want ./b/dep's file recorded under it alone:\n%s", dest, data, text)
						}
					} else {
						if _, err := os.Lstat(full); !os.IsNotExist(err) {
							t.Errorf("uninstall wrote %s (%v)", dest, err)
						}
						if cHashes[dest] != cHash || bRecords {
							t.Errorf("want %s recorded under C alone with %s:\n%s", dest, cHash, text)
						}
					}
					s.run(t, "install", "--frozen")
					data, err := os.ReadFile(full)
					if err != nil {
						t.Fatalf("frozen install did not restore %s: %v", dest, err)
					}
					if sum := sha256.Sum256(data); "sha256:"+hex.EncodeToString(sum[:]) != want {
						t.Errorf("%s = %q after the frozen install, want the content of %s", dest, data, want)
					}
					before, _, _, _ := s.lock(t)
					s.run(t, "install")
					if after, _, _, _ := s.lock(t); after != before {
						t.Errorf("install changed apm.lock.yaml:\n%s\nwas:\n%s", after, before)
					}
				})
			}
		}
	}
}

func copilotPromptDest(global bool) string {
	if global {
		return ".copilot/prompts/cy.prompt.md"
	}
	return ".github/prompts/cy.prompt.md"
}

func scopeName(global bool) string {
	if global {
		return "global"
	}
	return "project"
}

func hasLine(lines []string, s string) bool {
	for _, l := range lines {
		if l == s {
			return true
		}
	}
	return false
}

// newGainedState is ./b/dep (a skill sb) and C, which has the primitive at
// source, installed for target, then ./a/dep added first to apm.yml and the
// lock, as newOtherState adds it. C is ./c/other, or with local the project's
// own .apm/ content, the project being named project.
func newGainedState(t *testing.T, global, local, cFirst bool, target, project, source, body string) *otherState {
	t.Helper()
	s := newOtherScope(t, global, local)
	writeCompileFile(t, s.srcRoot, "b/dep/"+otherSource("skill", "sb"), otherBody("skill", "sb", "b"))
	writeCompileFile(t, s.cDir(), source, body)
	s.installThenAddA(t, "name: "+project+"\nversion: 1.0.0\ntargets: ["+target+"]\ndependencies:\n  apm:\n", cFirst, nil)
	return s
}
