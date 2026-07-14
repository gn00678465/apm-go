package bundle

import (
	"fmt"
	"strings"
)

// fileEntry is one file_map slot: the winning source path plus the owner
// name that wrote it (for collision messages).
type fileEntry struct {
	Source string
	Owner  string
}

// FileMap accumulates output-relative-path -> (source, owner) across every
// MergeFileMap call, mirroring plugin_exporter.py's file_map dict
// (§3.2/§3.3). Collisions accumulates every collision message encountered,
// in call order.
type FileMap struct {
	entries    map[string]fileEntry
	Collisions []string
}

func NewFileMap() *FileMap {
	return &FileMap{entries: make(map[string]fileEntry)}
}

// Keys returns every output-relative path currently in the map.
func (fm *FileMap) Keys() []string {
	keys := make([]string, 0, len(fm.entries))
	for k := range fm.entries {
		keys = append(keys, k)
	}
	return keys
}

// Source returns the winning source path for key, or "" if key is absent.
func (fm *FileMap) Source(key string) (string, bool) {
	e, ok := fm.entries[key]
	return e.Source, ok
}

// MergeFileMap merges components into fm under owner, mirroring
// _merge_file_map (plugin_exporter.py:683-709): without force, the first
// writer of any output-relative path wins and every subsequent collision is
// recorded but skipped; with force, the last writer wins and the collision
// is still recorded (force only changes who wins, never silences the
// warning). validOutputRel mirrors _validate_output_rel
// (plugin_exporter.py:40-46): an absolute or ".."-traversing output path is
// silently dropped rather than merged.
//
// This single function is reused for BOTH dep-vs-dep merging (called once
// per dependency, in lockfile order) AND the final root-package merge
// (called once more, last) -- Python's export_plugin_bundle uses the exact
// same _merge_file_map call for both (plugin_exporter.py:510,526), so
// "dep beats root package without --force" falls directly out of deps
// being merged in BEFORE the root package, not from any owner-specific
// branch in the merge logic itself.
func (fm *FileMap) MergeFileMap(components []Component, owner string, force bool) {
	for _, c := range components {
		if !validOutputRel(c.OutputRel) {
			continue
		}
		existing, exists := fm.entries[c.OutputRel]
		if !exists {
			fm.entries[c.OutputRel] = fileEntry{Source: c.Source, Owner: owner}
			continue
		}
		verb := "first writer wins"
		if force {
			verb = "last writer wins"
		}
		fm.Collisions = append(fm.Collisions, fmt.Sprintf(
			"%s -- collision between %q and %q (%s)", c.OutputRel, existing.Owner, owner, verb))
		if force {
			fm.entries[c.OutputRel] = fileEntry{Source: c.Source, Owner: owner}
		}
	}
}

// validOutputRel mirrors _validate_output_rel (plugin_exporter.py:40-46):
// rejects an absolute path (POSIX or Windows-drive form) or any path
// containing a literal ".." component (checked against the path AS GIVEN,
// not a resolved/cleaned form -- matching Python's
// "\"..\" not in Path(rel).parts").
func validOutputRel(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return false
	}
	if len(rel) >= 2 && rel[1] == ':' {
		return false // Windows drive-letter form, e.g. "C:\..."
	}
	for _, seg := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return false
		}
	}
	return true
}
