package main

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/ux"
)

// localDirWithoutMarkers returns the directory a local package ref names
// when it is a directory with none of the Oracle's package markers: an
// apm.yml, a SKILL.md or a plugin manifest find_plugin_json finds
// (install/validation.py:241-263). It reads the path as normalizeLocalDep
// does, on a copy.
func localDirWithoutMarkers(ref *manifest.DependencyReference) (string, bool) {
	c := *ref
	normalizeLocalDep(&c)
	if c.LocalSourcePath == "" {
		return "", false
	}
	info, err := os.Stat(c.LocalSourcePath)
	if err != nil || !info.IsDir() || hasPackageMarker(c.LocalSourcePath) {
		return "", false
	}
	return c.LocalSourcePath, true
}

func hasPackageMarker(dir string) bool {
	for _, m := range []string{"apm.yml", "SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return pluginJSONPath(dir) != ""
}

// printNoMarkersHint is the Oracle's _local_path_no_markers_hint
// (install/validation.py:203-238): the directories one and two levels below
// dir that have a package marker, the hint line when there is one, and with
// verbose the first five as commands to run. Deviation: the Oracle's line
// holds a second "[i]" inside the first; apm-go prints its one symbol.
func printNoMarkersHint(dir string, verbose bool) {
	var found []string
	for _, child := range sortedSubdirs(dir) {
		if hasPackageMarker(child) {
			found = append(found, child)
		}
		for _, grandchild := range sortedSubdirs(child) {
			if hasPackageMarker(grandchild) {
				found = append(found, grandchild)
			}
		}
	}
	if len(found) == 0 {
		return
	}
	ux.Info(os.Stdout, "  Found installable package(s) inside this directory:")
	if !verbose {
		return
	}
	for _, p := range found[:min(5, len(found))] {
		ux.Plain(os.Stdout, "      apm-go install %s", p)
	}
	if len(found) > 5 {
		ux.Plain(os.Stdout, "      ... and %d more", len(found)-5)
	}
}

// sortedSubdirs is the directories directly in dir, sorted by name, as
// sorted(Path.iterdir()) with is_dir() gives them.
func sortedSubdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			dirs = append(dirs, p)
		}
	}
	slices.Sort(dirs)
	return dirs
}
