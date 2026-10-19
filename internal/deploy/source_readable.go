package deploy

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// sourceReadFailure is one source location that exists but cannot be read.
type sourceReadFailure struct {
	path string
	err  error
}

// The checks in this file repeat the reads of CollectLocalPrimitives,
// CollectDependencyPrimitives, collectFromAPMDir and collectPluginPrimitives,
// which treat every read error as "no source here". Only the stale-file
// cleanup needs to tell an unreadable source from a missing one, and the
// collectors have many other callers (compile among them), so the check
// lives apart from them instead of changing what they return.
//
// Keep it in step with the collectors: a directory or file they start to
// read must be added here, or an unreadable one will again look deleted.

// apmFlatSubdirs are the .apm/ subdirectories collectFromAPMDir lists.
var apmFlatSubdirs = []string{"instructions", "agents", "commands", "hooks", "prompts"}

// rootFlatSubdirs are the directories at a package's root collectFromAPMDir
// lists, besides the root itself.
var rootFlatSubdirs = []string{"hooks"}

// readFailure returns nil for a successful read and for a missing path: a
// source that is not there is the normal case.
func readFailure(path string, err error) *sourceReadFailure {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	return &sourceReadFailure{path: path, err: err}
}

func dirReadFailure(dir string) *sourceReadFailure {
	_, err := os.ReadDir(dir)
	return readFailure(dir, err)
}

func statFailure(path string) *sourceReadFailure {
	_, err := os.Stat(path)
	return readFailure(path, err)
}

// skillsDirReadFailure covers a <skills>/<name>/SKILL.md scan.
func skillsDirReadFailure(skillsDir string) *sourceReadFailure {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return readFailure(skillsDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if f := statFailure(filepath.Join(skillsDir, e.Name(), "SKILL.md")); f != nil {
			return f
		}
	}
	return nil
}

func apmDirReadFailure(apmDir string) *sourceReadFailure {
	for _, sub := range apmFlatSubdirs {
		if f := dirReadFailure(filepath.Join(apmDir, sub)); f != nil {
			return f
		}
	}
	return skillsDirReadFailure(filepath.Join(apmDir, "skills"))
}

// rootReadFailure covers what collectFromAPMDir reads at a package's root:
// the root itself, for its prompts and agents, and rootFlatSubdirs.
func rootReadFailure(dir string) *sourceReadFailure {
	if f := dirReadFailure(dir); f != nil {
		return f
	}
	for _, sub := range rootFlatSubdirs {
		if f := dirReadFailure(filepath.Join(dir, sub)); f != nil {
			return f
		}
	}
	return nil
}

// localSourceReadFailure reports the first unreadable source location of the
// own content CollectScopeLocalPrimitives reads, or nil: its .apm/, and
// outside global what it reads at the project's root.
func localSourceReadFailure(projectDir string, global bool) *sourceReadFailure {
	if info, err := os.Stat(filepath.Join(projectDir, ".apm")); !global && err == nil && info.IsDir() {
		if f := rootReadFailure(projectDir); f != nil {
			return f
		}
	}
	return apmDirReadFailure(filepath.Join(projectDir, ".apm"))
}

// dependencySourceReadFailure reports the first unreadable source location of
// the dependency vendored at modulePath, or nil.
func dependencySourceReadFailure(modulePath string) *sourceReadFailure {
	if f := rootReadFailure(modulePath); f != nil {
		return f
	}
	if f := apmDirReadFailure(filepath.Join(modulePath, ".apm")); f != nil {
		return f
	}
	if f := statFailure(filepath.Join(modulePath, "SKILL.md")); f != nil {
		return f
	}
	skillsDeclared, f := pluginSourceReadFailure(modulePath)
	if f != nil {
		return f
	}
	if skillsDeclared {
		return nil
	}
	return skillsDirReadFailure(filepath.Join(modulePath, "skills"))
}

// pluginSourceReadFailure covers collectPluginPrimitives. A manifest that is
// readable but invalid, and a declared path resolvePluginPath rejects, are
// content decisions, not read failures.
func pluginSourceReadFailure(modulePath string) (skillsDeclared bool, failure *sourceReadFailure) {
	t := sourceTree{dir: modulePath}
	m, f, _ := findPluginManifest(t)
	if m == nil {
		return false, f
	}
	skillsDeclared = isJSONKeyPresent(m.Skills)

	for _, rel := range decodeStringOrList(m.Skills) {
		abs, ok := resolvePluginPath(modulePath, rel)
		if !ok {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			if f := readFailure(abs, err); f != nil {
				return skillsDeclared, f
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		if f := statFailure(filepath.Join(abs, "SKILL.md")); f != nil {
			return skillsDeclared, f
		}
	}

	apmPackage := isAPMPackage(t)
	flat := append(componentPaths(m.Agents, "agents", !apmPackage), componentPaths(m.Commands, "commands", !apmPackage)...)
	for _, rel := range flat {
		abs, ok := resolvePluginPath(modulePath, rel)
		if !ok {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			if f := readFailure(abs, err); f != nil {
				return skillsDeclared, f
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		if f := dirReadFailure(abs); f != nil {
			return skillsDeclared, f
		}
	}
	return skillsDeclared, nil
}
