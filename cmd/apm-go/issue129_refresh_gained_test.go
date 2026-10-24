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
// package that stays (or the project's own content) records, goes to the
// package the trial gives it, as the next install gives it: with ./b/dep
// declared first, ./b/dep's file is written there and recorded under it
// alone, whether C's file is still there (kept), has ./b/dep's content
// (same) or was removed by the user (miss); otherwise nothing is written
// there and C keeps its record alone, so a frozen install restores a removed
// file. Deviation: Oracle 0.33.0 keeps C's file and records the path under
// both packages, with C's hash (review runs r4/p2-cgainmiss-or and
// r5/cgain), and its --frozen, an ordinary install, passes; apm-go's frozen
// install would fail on that lock, which PRODUCT.md rules out. A copilot hook
// of a package is <package>-<stem>.json, so two packages' hooks share a path
// only when the project's own content is named like ./b/dep's directory, in a
// project (at user scope the project's own hooks are local-<stem>.json).
func TestUninstall_SharedLocalSlot_GainedPathOfAnotherPackage(t *testing.T) {
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
			for _, cFile := range []string{"miss", "kept", "same"} {
				for _, kind := range []string{"skill", "agent", "instr", "cmd", "prompt", "hook"} {
					if kind == "hook" && (global || order != "local") {
						continue
					}
					t.Run(scopeName(global)+"/"+order+"/"+cFile+"/"+kind, func(t *testing.T) {
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
						bBody := body("b")
						if cFile == "same" {
							bBody = body("c")
						}
						writeCompileFile(t, filepath.Join(s.srcRoot, "b", "dep"), g.source, bBody)
						switch {
						case cFile != "miss":
						case kind == "skill":
							mustRemoveAll(t, filepath.Join(s.deployRoot, ".claude", "skills", "sy"))
						default:
							mustRemove(t, full)
						}
						c := s.settled(t)
						c.roots = []string{".claude/"}
						if g.target == "copilot" {
							c.roots = []string{".github/", ".agents/skills/"}
							if global {
								c.roots = []string{".copilot/", ".agents/skills/"}
							}
						}
						s.run(t, "uninstall", s.a)
						text, b, cFiles, cHashes := s.lock(t)
						bRecords := slices.ContainsFunc(b.DeployedFiles, func(f string) bool { return f == dest || kind == "skill" && strings.HasPrefix(f, ".claude/skills/sy") })
						cRecords := slices.ContainsFunc(cFiles, func(f string) bool { return f == dest || kind == "skill" && strings.HasPrefix(f, ".claude/skills/sy") })
						want := cHash
						if order == "clast" {
							data, err := os.ReadFile(full)
							if err != nil {
								t.Fatalf("uninstall did not write %s: %v", dest, err)
							}
							sum := sha256.Sum256(data)
							want = "sha256:" + hex.EncodeToString(sum[:])
							who := " b"
							if cFile == "same" {
								who = " c"
							}
							if (want == cHash) != (cFile == "same") || !strings.Contains(string(data), who) || b.DeployedHashes[dest] != want || cRecords {
								t.Errorf("%s = %q, want ./b/dep's file recorded under it alone:\n%s", dest, data, text)
							}
						} else {
							if cFile == "miss" {
								if _, err := os.Lstat(full); !os.IsNotExist(err) {
									t.Errorf("uninstall wrote %s (%v)", dest, err)
								}
								// The user removed C's file; install puts it back.
								c.restored = []string{dest}
								if kind == "skill" {
									c.restored = []string{".claude/skills/sy"}
								}
							} else if data, err := os.ReadFile(full); err != nil || "sha256:"+hex.EncodeToString(sha256Sum(data)) != cHash {
								t.Errorf("%s = %q (%v), want C's file", dest, data, err)
							}
							if cHashes[dest] != cHash || bRecords {
								t.Errorf("want %s recorded under C alone with %s:\n%s", dest, cHash, text)
							}
						}
						c.check(t)
						data, err := os.ReadFile(full)
						if err != nil {
							t.Fatalf("frozen install did not restore %s: %v", dest, err)
						}
						if "sha256:"+hex.EncodeToString(sha256Sum(data)) != want {
							t.Errorf("%s = %q after the frozen install, want the content of %s", dest, data, want)
						}
					})
				}
			}
		}
	}
}

func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
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
