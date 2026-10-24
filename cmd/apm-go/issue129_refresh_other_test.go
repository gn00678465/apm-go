package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// Expected hashes are of the files Oracle 0.33.0 writes for ./c/other's
// primitives ("... c" content), measured for the third review of PR #171
// (runs below /home/madao/.cache/apm-go-oracle-ref/B/r3-*).
var otherDeployed = map[string]string{
	"skill": "d6280d7c6e4dc289b0afbddb1e57502814efef212af985f31f774f38889251da", // skills/sb/SKILL.md
	"agent": "beda61fe6690a9359afd72cb49e6c7b81bbfbb9bc0ee7e80f8b0a9ff063e9339", // agents/rb.md
	"instr": "e0e6d4ab5cda3b317aad83a8014ff8567f2497c5cb5a49622b1927e56a9cdbd8", // rules/ib.md, rules/iy.md
	"cmd":   "a3a42046a8b4b1e6642909553012d5d2b3151fed6567731fa7aa7f48b5f1c0c6", // commands/cb.md, commands/cy.md
	"sy":    "c7d2a31ab3ae48de9cbf364b8005b5d19108a3ce33871e86701522a80ec283a9",
	"ry":    "3c90035aab3ff8ce12439254d7ed59dc350110542e439e3486177ff682766b2d",
}

var otherKinds = []string{"skill", "agent", "instr", "cmd"}

// otherSource and otherDest are a primitive of kind and name, as a source
// below a package and as the path claude deploys it to.
func otherSource(kind, name string) string {
	return map[string]string{
		"skill": ".apm/skills/" + name + "/SKILL.md",
		"agent": ".apm/agents/" + name + ".agent.md",
		"instr": ".apm/instructions/" + name + ".instructions.md",
		"cmd":   ".apm/prompts/" + name + ".prompt.md",
	}[kind]
}

func otherDest(kind, name string) string {
	return map[string]string{
		"skill": ".claude/skills/" + name + "/SKILL.md",
		"agent": ".claude/agents/" + name + ".md",
		"instr": ".claude/rules/" + name + ".md",
		"cmd":   ".claude/commands/" + name + ".md",
	}[kind]
}

func otherBody(kind, name, who string) string {
	return map[string]string{
		"skill": "---\nname: " + name + "\ndescription: d\n---\nskill " + who + "\n",
		"agent": "---\nname: " + name + "\ndescription: d\n---\nagent " + who + "\n",
		"instr": "---\ndescription: d\n---\ninstruction " + who + "\n",
		"cmd":   "---\ndescription: d\n---\nprompt " + who + "\n",
	}[kind]
}

// shared and gained are the names ./b/dep has before the change and the names
// it may gain, one per kind.
var (
	sharedNames = map[string]string{"skill": "sb", "agent": "rb", "instr": "ib", "cmd": "cb"}
	gainedNames = map[string]string{"skill": "sy", "agent": "ry", "instr": "iy", "cmd": "cy"}
)

func otherHash(kind, name string) string {
	if h, ok := otherDeployed[name]; ok {
		return h
	}
	return otherDeployed[kind]
}

// otherState is ./b/dep (sb, rb, ib, cb) installed before C, which has the
// names b may gain (sy, ry, iy, cy) and b's names, so b records the shared
// names and C the others, and ./a/dep, which shares
// b's directory below apm_modules, is added first to apm.yml and the lock. C
// is ./c/other, or with local the project's own .apm/ content, which gets b's
// names after the install.
type otherState struct {
	deployRoot, manifestDir, srcRoot string
	a, b, c                          string
	global, local                    bool
}

func newOtherState(t *testing.T, global, local, cFirst bool) *otherState {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("APM_CONFIG_DIR", filepath.Join(root, "config"))
	s := &otherState{deployRoot: filepath.Join(root, "proj"), global: global, local: local}
	s.manifestDir, s.srcRoot = s.deployRoot, s.deployRoot
	if global {
		s.deployRoot, s.manifestDir, s.srcRoot = home, filepath.Join(home, ".apm"), filepath.Join(root, "src")
		oldDir, oldDeploy := globalDirOverride, globalDeployDirOverride
		globalDirOverride, globalDeployDirOverride = s.manifestDir, home
		t.Cleanup(func() { globalDirOverride, globalDeployDirOverride = oldDir, oldDeploy })
		s.a, s.b, s.c = filepath.Join(s.srcRoot, "a", "dep"), filepath.Join(s.srcRoot, "b", "dep"), filepath.Join(s.srcRoot, "c", "other")
	} else {
		s.a, s.b, s.c = "./a/dep", "./b/dep", "./c/other"
	}
	writeCompileFile(t, s.srcRoot, "a/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	writeCompileFile(t, s.srcRoot, "a/dep/.apm/skills/sa/SKILL.md", "---\nname: sa\ndescription: d\n---\nfrom a\n")
	writeCompileFile(t, s.srcRoot, "b/dep/apm.yml", "name: dep\nversion: 1.0.0\n")
	for _, k := range otherKinds {
		writeCompileFile(t, s.srcRoot, "b/dep/"+otherSource(k, sharedNames[k]), otherBody(k, sharedNames[k], "b"))
	}
	if !local {
		writeCompileFile(t, s.srcRoot, "c/other/apm.yml", "name: other\nversion: 1.0.0\n")
	}
	for _, k := range otherKinds {
		writeCompileFile(t, s.cDir(), otherSource(k, gainedNames[k]), otherBody(k, gainedNames[k], "c"))
		if !local {
			writeCompileFile(t, s.cDir(), otherSource(k, sharedNames[k]), otherBody(k, sharedNames[k], "c"))
		}
	}
	entries := func(paths ...string) string {
		out := "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n"
		for _, p := range paths {
			out += "    - '" + p + "'\n"
		}
		return out
	}
	rest := []string{s.b, s.c}
	if local {
		rest = []string{s.b}
	}
	writeCompileFile(t, s.manifestDir, "apm.yml", entries(rest...))
	if global {
		chdirTemp(t)
		mustRunCLI(t, "install", "-g")
	} else {
		t.Chdir(s.deployRoot)
		mustRunCLI(t, "install")
	}
	if local {
		// The project's own content would win b's names at the install.
		for _, k := range otherKinds {
			writeCompileFile(t, s.cDir(), otherSource(k, sharedNames[k]), otherBody(k, sharedNames[k], "c"))
		}
	}
	if cFirst && !local {
		rest = []string{s.c, s.b}
	}
	writeCompileFile(t, s.manifestDir, "apm.yml", entries(append([]string{s.a}, rest...)...))
	lockPath := filepath.Join(s.manifestDir, "apm.lock.yaml")
	lock := strings.Replace(readFileString(t, lockPath), "dependencies:\n", "dependencies:\n- repo_url: _local/dep\n  source: local\n  local_path: '"+s.a+"'\n", 1)
	writeCompileFile(t, s.manifestDir, "apm.lock.yaml", lock)
	return s
}

// cDir is the directory C's primitives are below.
func (s *otherState) cDir() string {
	if s.local {
		return s.manifestDir
	}
	return filepath.Join(s.srcRoot, "c", "other")
}

func (s *otherState) run(t *testing.T, args ...string) string {
	t.Helper()
	if s.global {
		args = append([]string{args[0], "-g"}, args[1:]...)
	}
	return mustRunCLI(t, args...)
}

func (s *otherState) lock(t *testing.T) (text string, b *lockfile.LockedDep, cFiles []string, cHashes map[string]string) {
	t.Helper()
	text = readFileString(t, filepath.Join(s.manifestDir, "apm.lock.yaml"))
	node, err := yamlcore.SafeLoad([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	lf, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	cFiles, cHashes = lf.LocalDeployedFiles, lf.LocalDeployedHashes
	for i := range lf.Dependencies {
		d := &lf.Dependencies[i]
		switch {
		case d.RepoURL == "_local/dep":
			b = d
		case d.RepoURL == "_local/other" && !s.local:
			cFiles, cHashes = d.DeployedFiles, d.DeployedHashes
		}
	}
	if b == nil {
		t.Fatalf("apm.lock.yaml has no entry for ./b/dep:\n%s", text)
	}
	return text, b, cFiles, cHashes
}

// expect checks that each path has C's content on disk and in the lock,
// recorded under C and not under ./b/dep.
func (s *otherState) expect(t *testing.T, paths map[string]string) {
	t.Helper()
	text, b, cFiles, cHashes := s.lock(t)
	for p, h := range paths {
		data, err := os.ReadFile(filepath.Join(s.deployRoot, filepath.FromSlash(p)))
		if err != nil {
			t.Errorf("read %s: %v", p, err)
		} else if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != h {
			t.Errorf("%s = %q, want the content of hash %s", p, data, h)
		}
		if !strings.Contains(strings.Join(cFiles, "\n")+"\n", p+"\n") || strings.TrimPrefix(cHashes[p], "sha256:") != h {
			t.Errorf("C does not record %s with hash %s (hash %q):\n%s", p, h, cHashes[p], text)
		}
		for _, f := range b.DeployedFiles {
			if f == p {
				t.Errorf("./b/dep records %s too:\n%s", p, text)
			}
		}
	}
}

// When the package that stays in a slot two declarations shared changes, a
// path its new copy no longer deploys and another package that stays has a
// primitive for is that package's: it is written with that package's content
// and recorded under it, as Oracle 0.33.0 redeploys every remaining package
// (measured, r3-{cfirst,clast}-shadow-*, r3-g-*-shadow-*). A path its new copy
// gains that another package records stays that package's, on disk and in the
// lock (measured, r3-*-gain-*). With C declared first, every path of b's
// names goes to C, whatever b changed. With C after b, b keeps the names it
// still has. The project's own content is C too (r3-local-*). Deviations: the
// Oracle records a gained skill or rule under both packages, and deletes the
// project's own agent of a name b had (r3-local-shadow-agent).
func TestUninstall_SharedLocalSlot_OtherPackageOfTheSameName(t *testing.T) {
	for _, scope := range []struct {
		name   string
		global bool
	}{{"project", false}, {"global", true}} {
		for _, order := range []string{"cfirst", "clast", "local"} {
			for _, change := range []string{"shadow", "gain"} {
				for _, kind := range otherKinds {
					t.Run(scope.name+"/"+order+"/"+change+"/"+kind, func(t *testing.T) {
						s := newOtherState(t, scope.global, order == "local", order == "cfirst")
						bSrc := filepath.Join(s.srcRoot, "b", "dep")
						want := map[string]string{}
						if change == "shadow" {
							src := otherSource(kind, sharedNames[kind])
							if kind == "skill" {
								src = filepath.Dir(src)
							}
							mustRemoveAll(t, filepath.Join(bSrc, src))
							want[otherDest(kind, sharedNames[kind])] = otherHash(kind, sharedNames[kind])
						} else {
							name := gainedNames[kind]
							writeCompileFile(t, bSrc, otherSource(kind, name), otherBody(kind, name, "b"))
							want[otherDest(kind, name)] = otherHash(kind, name)
						}
						if order != "clast" {
							for _, k := range otherKinds {
								want[otherDest(k, sharedNames[k])] = otherHash(k, sharedNames[k])
							}
						}
						s.run(t, "uninstall", s.a)
						s.expect(t, want)
						s.run(t, "install", "--frozen")
						if order == "clast" && change == "gain" {
							// b is declared first and now has the name, so the
							// next install gives it the path, as Oracle 0.33.0's
							// next install changes it too.
							return
						}
						before, _, _, _ := s.lock(t)
						s.run(t, "install")
						if after, _, _, _ := s.lock(t); after != before {
							t.Errorf("install changed apm.lock.yaml:\n%s\nwas:\n%s", after, before)
						}
						s.expect(t, want)
					})
				}
			}
		}
	}
}
