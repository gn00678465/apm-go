package pluginjson

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// StagedScaffold collects generated files in a temporary directory inside
// the project root and commits them as one unit, mirroring upstream's
// _write_and_validate_agent_plugin_scaffold + _commit_staged_scaffold
// (commands/init.py:406-475): on any failure every prior file is restored
// and nothing half-written is left behind.
type StagedScaffold struct {
	projectRoot string
	stageDir    string
	files       []string
	// keep is set when a rollback could not fully restore the prior files:
	// Cleanup then leaves the staging dir (and its .backup/) in place so
	// the user's original bytes are never lost (Round-2 F5).
	keep bool
}

// NewStagedScaffold creates the staging directory (".apm-plugin-init-*",
// same prefix as upstream) under projectRoot. Call Cleanup when done.
func NewStagedScaffold(projectRoot string) (*StagedScaffold, error) {
	dir, err := os.MkdirTemp(projectRoot, ".apm-plugin-init-")
	if err != nil {
		return nil, fmt.Errorf("create scaffold staging dir: %w", err)
	}
	return &StagedScaffold{projectRoot: projectRoot, stageDir: dir}, nil
}

// Dir is the staging directory; write files here via Add's path.
func (s *StagedScaffold) Dir() string { return s.stageDir }

// Add records name as part of the commit set and returns its staged path.
func (s *StagedScaffold) Add(name string) string {
	s.files = append(s.files, name)
	return filepath.Join(s.stageDir, name)
}

// Commit moves every staged file into projectRoot. If any move fails, files
// already committed are removed and any prior versions are restored, then
// the error is returned. If that rollback itself fails, the rollback errors
// are joined onto the original error, and the backup directory is kept so
// the prior files remain recoverable.
func (s *StagedScaffold) Commit() (err error) {
	backupRoot := filepath.Join(s.stageDir, ".backup")
	if err := os.Mkdir(backupRoot, 0o755); err != nil {
		return fmt.Errorf("create scaffold backup dir: %w", err)
	}
	backups := map[string]string{}
	var committed []string
	defer func() {
		if err == nil {
			return
		}
		var rb []error
		for _, name := range committed {
			if rmErr := os.Remove(filepath.Join(s.projectRoot, name)); rmErr != nil && !os.IsNotExist(rmErr) {
				rb = append(rb, fmt.Errorf("rollback: remove %s: %w", name, rmErr))
			}
		}
		for name, backup := range backups {
			if mvErr := os.Rename(backup, filepath.Join(s.projectRoot, name)); mvErr != nil {
				rb = append(rb, fmt.Errorf("rollback: restore %s: %w", name, mvErr))
			}
		}
		if len(rb) > 0 {
			s.keep = true
			rb = append(rb, fmt.Errorf("rollback incomplete; prior files preserved under %s", backupRoot))
			err = errors.Join(append([]error{err}, rb...)...)
		}
	}()

	for _, name := range s.files {
		dest := filepath.Join(s.projectRoot, name)
		if _, statErr := os.Lstat(dest); statErr == nil {
			backup := filepath.Join(backupRoot, name)
			if err = os.Rename(dest, backup); err != nil {
				return fmt.Errorf("back up %s: %w", name, err)
			}
			backups[name] = backup
		}
	}
	for _, name := range s.files {
		if commitHook != nil {
			if err = commitHook(name); err != nil {
				return fmt.Errorf("commit %s: %w", name, err)
			}
		}
		if err = os.Rename(filepath.Join(s.stageDir, name), filepath.Join(s.projectRoot, name)); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
		committed = append(committed, name)
	}
	return nil
}

// Cleanup removes the staging directory -- unless a failed rollback left
// prior files in its .backup/, in which case it is deliberately kept.
func (s *StagedScaffold) Cleanup() {
	if s.keep {
		return
	}
	os.RemoveAll(s.stageDir)
}
