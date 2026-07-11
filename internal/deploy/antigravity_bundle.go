package deploy

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// antigravityBundleRoot is the workspace customization root under which
// every dependency's antigravity plugin bundle lives:
// .agents/plugins/<pkg>/plugin.json + rules/agents/skills/hooks.json
// (research/cli-plugins.md:142-156). Local (project-owned) primitives never
// go here -- they keep the pre-existing flat paths (design.md D1).
const antigravityBundleRoot = ".agents/plugins"

// antigravityBundleDir returns the workspace-relative bundle directory for a
// dependency: .agents/plugins/<sanitized DepKey last segment>.
func antigravityBundleDir(depKey string) string {
	return path.Join(antigravityBundleRoot, bundleNameFromDepKey(depKey))
}

// bundleNameFromDepKey derives the plugin bundle directory name from a
// dependency's unique key, taking only the LAST "/"-separated segment
// (mirroring skillNameFromDepKey in primitive.go) so a materialized
// local-path dependency's "_local/<base>-<hash8>" key collapses to
// "<base>-<hash8>", and any earlier segment -- including one that happens to
// be ".." -- never reaches the filesystem call. The result is then reduced
// to a single safe path segment by sanitizeBundleSegment.
func bundleNameFromDepKey(depKey string) string {
	base := depKey
	if idx := strings.LastIndex(depKey, "/"); idx >= 0 {
		base = depKey[idx+1:]
	}
	return sanitizeBundleSegment(base)
}

// sanitizeBundleSegment reduces s to a single safe path segment: every
// character outside [A-Za-z0-9._-] becomes '-', and a result that is empty,
// ".", "..", or starts with "." is replaced with a "pkg-" prefixed
// fallback -- the same convention cmd/apm/install.go's sanitizePathSegment
// uses for apm_modules keys -- so the derived bundle directory name can
// never be empty, hidden, or a traversal segment.
func sanitizeBundleSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" || out == "." || out == ".." || strings.HasPrefix(out, ".") {
		out = "pkg-" + strings.TrimLeft(out, ".")
	}
	return out
}

// ValidateBundleNames implements BundleTarget. Two different dependencies
// whose bundle name sanitizes to the same value would otherwise have their
// rules/agents/skills/hooks files mixed into one physical directory, so this
// fails closed instead of just diagnosing -- deploy.go's Run calls it before
// any primitive is deployed to any target, so a collision here means nothing
// has been written yet.
func (a *antigravityAdapter) ValidateBundleNames(depKeys []string) error {
	seen := make(map[string]string, len(depKeys))
	for _, dk := range depKeys {
		name := bundleNameFromDepKey(dk)
		if prev, ok := seen[name]; ok && prev != dk {
			return fmt.Errorf(
				"antigravity plugin bundle name %q would be shared by dependency %q and %q; rename one dependency so their .agents/plugins/ bundle directories do not collide",
				name, prev, dk)
		}
		seen[name] = dk
	}
	return nil
}

// FinalizeBundles implements BundleTarget. It writes the minimal plugin.json
// manifest for every bundled dependency ({"name": "<bundle-name>"}, LF
// terminated, byte-deterministic) -- agy >=1.1.1 rejects a bundle whose
// plugin.json is missing or has no name (research/antigravity-bundle-notes.md
// G3/G5). It is called once per Run(), after every primitive has been
// deployed, and always rewrites and reports the manifest path so a
// re-install never drops it from deployed_files provenance.
func (a *antigravityAdapter) FinalizeBundles(depKeys []string, projectDir string) (map[string][]string, error) {
	out := make(map[string][]string, len(depKeys))
	for _, dk := range depKeys {
		name := bundleNameFromDepKey(dk)
		manifestRel := path.Join(antigravityBundleRoot, name, "plugin.json")
		absManifest := filepath.Join(projectDir, filepath.FromSlash(manifestRel))
		if err := os.MkdirAll(filepath.Dir(absManifest), 0755); err != nil {
			return nil, fmt.Errorf("create plugin bundle dir for %s: %w", dk, err)
		}
		body := fmt.Sprintf("{\"name\": %q}\n", name)
		if err := os.WriteFile(absManifest, []byte(body), 0644); err != nil {
			return nil, fmt.Errorf("write plugin manifest for %s: %w", dk, err)
		}
		out[dk] = []string{manifestRel}
	}
	return out, nil
}
