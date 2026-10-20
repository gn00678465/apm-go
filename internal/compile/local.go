package compile

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/deploy"
)

const instructionSuffix = ".instructions.md"

// skippedDirs is the Oracle's DEFAULT_SKIP_DIRS (constants.py:41-73): a file
// with one of these names among its directories is not read
// (primitives/discovery.py:645-648); CompileInventory does not even enter
// the first four (compilation/inventory.py:11-18). The names are compared as
// written, on Windows too. Nothing below such a directory is read, so the
// walk enters none of them.
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "__pycache__": true, ".pytest_cache": true,
	".venv": true, "venv": true, ".tox": true, "build": true, "dist": true,
	".mypy_cache": true, "apm_modules": true, "vendor": true, "third_party": true,
	"Pods": true, "bower_components": true, "jspm_packages": true, ".gradle": true,
	"target": true, ".next": true, ".nuxt": true, ".cache": true, ".turbo": true,
}

// collectLocalInstructions finds the project's own *.instructions.md the way
// the Oracle's compile does: the pattern **/*.instructions.md
// (primitives/discovery.py:35-39) over CompileInventory's walk
// (compilation/inventory.py:46-93). Each directory gives its files in name
// order, then its subdirectories in name order, depth first. A symlinked
// directory is not entered, and with go.mod's winsymlink=0 neither is a
// junction, where Python's os.walk enters one (ruling in issue #131 comment
// 6286949791). A directory other than the root that holds a .git is a nested
// repository and gives nothing. A file that is a symlink is not read
// (discovery.py:653).
func collectLocalInstructions(projectDir string) []deploy.Primitive {
	var out []deploy.Primitive
	walkLocalInstructions(projectDir, "", &out)
	return out
}

func walkLocalInstructions(projectDir, rel string, out *[]deploy.Primitive) {
	dir := filepath.Join(projectDir, rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	if rel != "" {
		for _, e := range entries {
			if e.Name() == ".git" {
				return
			}
		}
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), instructionSuffix)
		if !ok || name == "" || !e.Type().IsRegular() {
			continue
		}
		*out = append(*out, deploy.Primitive{
			Name:    name,
			Type:    deploy.TypeInstructions,
			Source:  "local",
			SrcPath: filepath.Join(dir, e.Name()),
		})
	}
	for _, e := range entries {
		child := filepath.Join(rel, e.Name())
		switch {
		case e.Type()&fs.ModeSymlink != 0 && isAPMInstructionsLink(child):
			*out = append(*out, linkedAPMInstructions(projectDir)...)
		case e.IsDir() && !skippedDirs[e.Name()]:
			walkLocalInstructions(projectDir, child, out)
		}
	}
}

// isAPMInstructionsLink is whether rel, a link, is .apm or .apm/instructions.
func isAPMInstructionsLink(rel string) bool {
	rel = filepath.ToSlash(rel)
	return rel == ".apm" || rel == ".apm/instructions"
}

// linkedAPMInstructions is what an earlier apm-go compiled when .apm or
// .apm/instructions is a symlink or a junction: the first level of
// .apm/instructions, read through the link. The Oracle reads nothing there;
// apm-go keeps the old reading (issue #131 comments 6286949791 and
// 6286973860). It is nil when neither is a link.
func linkedAPMInstructions(projectDir string) []deploy.Primitive {
	if !isLink(filepath.Join(projectDir, ".apm")) && !isLink(filepath.Join(projectDir, ".apm", "instructions")) {
		return nil
	}
	var out []deploy.Primitive
	for _, p := range deploy.CollectLocalPrimitives(projectDir) {
		if p.Type == deploy.TypeInstructions {
			out = append(out, p)
		}
	}
	return out
}

func isLink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&fs.ModeSymlink != 0
}

// apmHasInstruction is the Oracle's any(apm_dir.rglob("*.instructions.md"))
// (commands/compile/cli.py:601-611), which enters no symlinked directory and
// skips no name.
func apmHasInstruction(projectDir string) bool {
	found := false
	_ = filepath.WalkDir(filepath.Join(projectDir, ".apm"), func(_ string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), instructionSuffix) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
