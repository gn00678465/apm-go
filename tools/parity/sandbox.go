package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// sandbox is one run's isolated filesystem: a fresh cwd (fixture
// materialised into it), a fresh HOME, and a fresh APM_CONFIG_DIR, all under
// one temp root so cleanup() removes everything in a single call. The runner
// never points any of these at the invoking user's real $HOME or
// $APM_CONFIG_DIR.
type sandbox struct {
	root      string
	Cwd       string
	Home      string
	ConfigDir string
}

// newSandbox creates a fresh sandbox and, if fixtureDir is non-empty, copies
// its tree into the sandbox cwd.
func newSandbox(fixtureDir string) (*sandbox, error) {
	root, err := os.MkdirTemp("", "apm-parity-*")
	if err != nil {
		return nil, fmt.Errorf("creating sandbox root: %w", err)
	}

	sb := &sandbox{
		root:      root,
		Cwd:       filepath.Join(root, "cwd"),
		Home:      filepath.Join(root, "home"),
		ConfigDir: filepath.Join(root, "config"),
	}
	for _, dir := range []string{sb.Cwd, sb.Home, sb.ConfigDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			os.RemoveAll(root)
			return nil, fmt.Errorf("creating sandbox dir %s: %w", dir, err)
		}
	}

	if fixtureDir != "" {
		if err := copyTree(fixtureDir, sb.Cwd); err != nil {
			os.RemoveAll(root)
			return nil, fmt.Errorf("materialising fixture: %w", err)
		}
	}

	return sb, nil
}

// cleanup removes the entire sandbox root. Callers that need the sandbox
// contents after the run must copy them out first (writeEvidence does this).
func (sb *sandbox) cleanup() {
	os.RemoveAll(sb.root)
}

// copyTree recursively copies src into dst, preserving regular files,
// directories, and symlinks (as symlinks, not their target's content).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if rel == "." {
			return nil
		}

		switch {
		case d.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("reading symlink %s: %w", path, err)
			}
			return os.Symlink(link, target)
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		default:
			return copyFile(path, target, d)
		}
	})
}

func copyFile(src, dst string, d os.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
