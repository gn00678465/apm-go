package authoring

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/semver"
	"github.com/apm-go/apm/internal/yamlcore"
)

// `apm-go marketplace package update` (apm-go-only, issue #25): write the
// upgrade `marketplace outdated` reports for a SHA pin back into the
// authoring config. The Oracle has no such command; its `package set`
// cannot hold a display version and a SHA ref together, so the pair could
// only be edited by hand.

// UpdateAction is what the plan decided for one selected package.
type UpdateAction int

const (
	// UpdateSkip leaves the entry alone; Note says why.
	UpdateSkip UpdateAction = iota
	// UpdateApply writes NewRef, and NewVersion when the entry has a version.
	UpdateApply
)

// PackageUpdate is one selected package's planned outcome.
type PackageUpdate struct {
	// Index is the entry's position in the config's packages sequence.
	Index      int
	Package    PackageEntry
	Action     UpdateAction
	Note       string
	NewVersion string
	NewRef     string
}

// UpdateResolutionError reports the selected packages whose remote could
// not be resolved. Lines holds one "cannot update: package '<name>': <note>"
// per package, in config order, so the command can print each as its own
// status line.
type UpdateResolutionError struct {
	Lines []string
}

func (e *UpdateResolutionError) Error() string { return strings.Join(e.Lines, "\n") }

// PlanPackageUpdates decides, for the packages names selects (all of cfg's
// when names is empty), what `package update` writes. The verdict is
// outdatedForPackage's: an entry is updated exactly when `outdated` reports
// it as an upgradable SHA pin. The result is in config order.
//
// An unknown name is an error before any ListRefs call. A selected package
// whose remote cannot be resolved (outdated's [x]) fails the whole plan
// with an *UpdateResolutionError, so the caller never writes part of a
// batch.
func PlanPackageUpdates(cfg *AuthoringConfig, names []string, includePrerelease bool, lister RefLister) ([]PackageUpdate, error) {
	selected := make([]bool, len(cfg.Packages))
	for _, name := range names {
		idx := findPackageIndex(cfg, name)
		if idx < 0 {
			return nil, fmt.Errorf("package %q not found", name)
		}
		selected[idx] = true
	}

	var updates []PackageUpdate
	var failures []string
	for i, pkg := range cfg.Packages {
		if len(names) > 0 && !selected[i] {
			continue
		}
		u := PackageUpdate{Index: i, Package: pkg}
		if pkg.Ref == "" && !isLocalPackageSource(pkg.Source) {
			// outdated resolves a range with no ref against the remote's
			// tags, but there is no pin here to write a commit into.
			u.Note = "No ref pin; skipped"
			updates = append(updates, u)
			continue
		}
		row := outdatedForPackage(cfg, pkg, lister, false, includePrerelease, "")
		switch {
		case row.Status == "[x]":
			failures = append(failures, fmt.Sprintf("cannot update: package '%s': %s", pkg.Name, row.Note))
		case row.TargetRef != "":
			u.Action, u.NewRef = UpdateApply, row.TargetRef
			if row.TargetVersion != "" {
				old := strings.TrimSpace(pkg.Version)
				u.NewVersion = strings.TrimSuffix(old, semver.StripVPrefix(old)) + row.TargetVersion
			}
		case row.Status == "[+]":
			u.Note = "already up to date"
		default:
			u.Note = row.Note
		}
		updates = append(updates, u)
	}
	if len(failures) > 0 {
		return nil, &UpdateResolutionError{Lines: failures}
	}
	return updates, nil
}

// ApplyPackageUpdates writes every UpdateApply in updates to dir's active
// config file in one atomic write that keeps the file's permission bits,
// replacing only the bytes of each entry's `ref` and `version` values.
// `package set` renders the whole entry again (packageEntryNode), which
// drops comments and keys it does not know; an entry that cannot be edited
// in place is an error here, never a redraw. Nothing is written when
// updates holds no UpdateApply.
//
// dryRun does everything but the write, so it returns the error a real run
// would return for the same file.
func ApplyPackageUpdates(dir string, updates []PackageUpdate, dryRun bool) error {
	planned := make(map[int]PackageUpdate)
	for _, u := range updates {
		if u.Action == UpdateApply {
			planned[u.Index] = u
		}
	}
	if len(planned) == 0 {
		return nil
	}

	path, prefix, err := locateEditableConfig(dir)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	before, seq, err := parsePackagesForUpdate(src, prefix)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	out := src
	for _, u := range updates {
		if u.Action != UpdateApply {
			continue
		}
		// The plan was made from an earlier read of the file.
		if u.Index >= len(before) || !reflect.DeepEqual(before[u.Index], u.Package) {
			return fmt.Errorf("%s changed while the update was resolved; run the command again", path)
		}
		values := []struct{ key, value string }{{"ref", u.NewRef}}
		if u.NewVersion != "" {
			values = append(values, struct{ key, value string }{"version", u.NewVersion})
		}
		for _, v := range values {
			// Each replacement keeps the line count and no two of these
			// values share a line, so the positions SafeLoad(src) gave
			// stay valid for every later replacement.
			replaced, ok := yamlcore.ReplaceScalarValue(out, mappingValue(seq.Content[u.Index], v.key), v.value)
			if !ok {
				return fmt.Errorf("cannot update package '%s' in place: its %s is not a single-line value of a block mapping in %s; edit it by hand", u.Package.Name, v.key, path)
			}
			out = replaced
		}
	}

	after, _, err := parsePackagesForUpdate(out, prefix)
	if err == nil {
		err = packageEditValidate(out, prefix)
	}
	if err != nil {
		return fmt.Errorf("edit produced an invalid config, aborting without writing: %w", err)
	}
	want := make([]PackageEntry, len(before))
	for i, pkg := range before {
		if u, ok := planned[i]; ok {
			pkg.Ref = u.NewRef
			if u.NewVersion != "" {
				pkg.Version = u.NewVersion
			}
		}
		want[i] = pkg
	}
	if !reflect.DeepEqual(after, want) {
		return fmt.Errorf("edit of %s did not produce exactly the planned values, aborting without writing", path)
	}
	if dryRun {
		return nil
	}
	return writeConfigKeepingMode(path, out)
}

// writeConfigKeepingMode replaces path's content the way atomicWriteFile
// does (temp file in the same directory, fsync, rename), with path's
// permission bits put on the temp file before the rename. It is separate
// because atomicWriteFile leaves the file with CreateTemp's 0600, and
// changing that function for `package add/set/remove` needs an owner
// ruling.
func writeConfigKeepingMode(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	step := "write temp file for"
	_, err = tmp.Write(data)
	if err == nil {
		step, err = "chmod temp file for", tmp.Chmod(info.Mode().Perm())
	}
	if err == nil {
		step, err = "fsync temp file for", tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		step, err = "close temp file for", closeErr
	}
	if err == nil {
		step, err = "commit write to", os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("%s %s: %w", step, path, err)
	}
	return nil
}

// parsePackagesForUpdate reads data the way LoadAuthoringConfig reads the
// file at prefix, returning the parsed packages together with the packages
// sequence node they came from. parsePackages yields one entry per element,
// so the two have the same order and length.
func parsePackagesForUpdate(data []byte, prefix []string) ([]PackageEntry, *yaml.Node, error) {
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, nil, err
	}
	block := doc.Content[0]
	for _, key := range prefix {
		block = mappingValue(block, key)
	}
	seq := mappingValue(block, "packages")
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil, nil, fmt.Errorf("config has no packages sequence")
	}
	cfg, err := parseAuthoringNode(block, topLevelFields{}, len(prefix) == 0)
	if err != nil {
		return nil, nil, err
	}
	return cfg.Packages, seq, nil
}
