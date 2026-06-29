package gitops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// RealPackageLoader implements resolver.PackageLoader via git clone.
type RealPackageLoader struct {
	ModulesDir  string
	DefaultHost string
	Lock        *lockfile.Lockfile
}

func (r *RealPackageLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	if ref.IsLocal {
		return r.loadLocalPackage(ref.LocalPath)
	}

	installDir := r.installPath(ref)

	if _, err := os.Stat(installDir); os.IsNotExist(err) {
		cloneURL := r.resolveCloneURL(ref)
		if err := r.cloneRepo(cloneURL, installDir, resolvedRef); err != nil {
			return nil, fmt.Errorf("clone %s: %w", cloneURL, err)
		}
	}

	return r.parseSubManifest(installDir)
}

func (r *RealPackageLoader) installPath(ref *manifest.DependencyReference) string {
	key := ref.RepoURL
	if ref.VirtualPath != "" {
		key += "/" + ref.VirtualPath
	}
	safe := strings.ReplaceAll(key, "/", string(filepath.Separator))
	return filepath.Join(r.ModulesDir, safe)
}

func (r *RealPackageLoader) resolveCloneURL(ref *manifest.DependencyReference) string {
	if ref.Scheme != "" {
		switch ref.Scheme {
		case "https", "http":
			host := ref.Host
			if host == "" {
				host = r.defaultHost()
			}
			return ref.Scheme + "://" + host + "/" + ref.Owner + "/" + ref.Repo + ".git"
		case "ssh":
			host := ref.Host
			if host == "" {
				host = r.defaultHost()
			}
			return "ssh://git@" + host + "/" + ref.Owner + "/" + ref.Repo + ".git"
		case "git":
			host := ref.Host
			if host == "" {
				host = r.defaultHost()
			}
			return "git@" + host + ":" + ref.Owner + "/" + ref.Repo + ".git"
		}
	}
	host := ref.Host
	if host == "" {
		host = r.defaultHost()
	}
	return "https://" + host + "/" + ref.Owner + "/" + ref.Repo + ".git"
}

func (r *RealPackageLoader) defaultHost() string {
	if r.DefaultHost != "" {
		return r.DefaultHost
	}
	return "github.com"
}

func (r *RealPackageLoader) cloneRepo(url, dir, ref string) error {
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, dir)

	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\n%s", err, string(out))
	}
	return nil
}

func (r *RealPackageLoader) loadLocalPackage(path string) (*manifest.Manifest, error) {
	return r.parseSubManifest(path)
}

func (r *RealPackageLoader) parseSubManifest(dir string) (*manifest.Manifest, error) {
	apmYml := filepath.Join(dir, "apm.yml")
	data, err := os.ReadFile(apmYml)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", apmYml, err)
	}

	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		return nil, fmt.Errorf("validate %s: %w", apmYml, err)
	}
	return m, nil
}
