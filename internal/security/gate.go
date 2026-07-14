package security

import (
	"io/fs"
	"path/filepath"
)

// ScanPolicy declares how a command handles security findings, mirroring
// gate.py's ScanPolicy dataclass.
type ScanPolicy struct {
	// OnCritical is "block" (exits/blocks deployment), "warn" (continues
	// with a warning), or "ignore" (collects findings silently).
	OnCritical string
	// ForceOverrides: when true, force=true downgrades "block" to "warn".
	ForceOverrides bool
}

const (
	OnCriticalBlock  = "block"
	OnCriticalWarn   = "warn"
	OnCriticalIgnore = "ignore"
)

// EffectiveBlock reports whether this policy would block deployment given
// force, mirroring ScanPolicy.effective_block.
func (p ScanPolicy) EffectiveBlock(force bool) bool {
	return p.OnCritical == OnCriticalBlock && !(p.ForceOverrides && force)
}

// Pre-built policies -- use these instead of constructing ad-hoc ones,
// mirroring gate.py's BLOCK_POLICY/WARN_POLICY/REPORT_POLICY.
var (
	BlockPolicy  = ScanPolicy{OnCritical: OnCriticalBlock, ForceOverrides: true}
	WarnPolicy   = ScanPolicy{OnCritical: OnCriticalWarn, ForceOverrides: false}
	ReportPolicy = ScanPolicy{OnCritical: OnCriticalIgnore, ForceOverrides: false}
)

// ScanVerdict is the result of a SecurityGate check, mirroring gate.py's
// ScanVerdict dataclass.
type ScanVerdict struct {
	FindingsByFile map[string][]ScanFinding
	HasCritical    bool
	ShouldBlock    bool
	CriticalCount  int
	WarningCount   int
	FilesScanned   int
}

// AllFindings flattens findings across all files, mirroring
// ScanVerdict.all_findings.
func (v ScanVerdict) AllFindings() []ScanFinding {
	var all []ScanFinding
	for _, f := range v.FindingsByFile {
		all = append(all, f...)
	}
	return all
}

// SecurityGate is the single entry point for security scanning across all
// commands, mirroring gate.py's SecurityGate class (a namespace of static
// methods in Python; a zero-value struct with methods in Go).
type SecurityGate struct{}

// ScanFiles walks root and scans every regular file, returning a verdict.
// Symlinks are never followed, mirroring scan_files(followlinks=False).
func (SecurityGate) ScanFiles(root string, policy ScanPolicy, force bool) ScanVerdict {
	findingsByFile := make(map[string][]ScanFinding)
	filesScanned := 0

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Mirror os.walk's default onerror=None: skip unreadable
			// entries but keep walking rather than aborting the scan.
			return nil
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		filesScanned++
		if findings := ScanFile(path); len(findings) > 0 {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			findingsByFile[filepath.ToSlash(rel)] = findings
		}
		return nil
	})

	return buildVerdict(findingsByFile, filesScanned, policy, force)
}

// ScanText scans in-memory text (compiled output, generated files),
// mirroring SecurityGate.scan_text.
func (SecurityGate) ScanText(content, filename string, policy ScanPolicy) ScanVerdict {
	fileFindings := ScanText(content, filename)
	findingsByFile := make(map[string][]ScanFinding)
	if len(fileFindings) > 0 {
		findingsByFile[filename] = fileFindings
	}
	return buildVerdict(findingsByFile, 1, policy, false)
}

func buildVerdict(findingsByFile map[string][]ScanFinding, filesScanned int, policy ScanPolicy, force bool) ScanVerdict {
	if len(findingsByFile) == 0 {
		return ScanVerdict{FilesScanned: filesScanned}
	}

	var flat []ScanFinding
	for _, f := range findingsByFile {
		flat = append(flat, f...)
	}
	hasCritical, counts := Classify(flat)
	shouldBlock := hasCritical && policy.EffectiveBlock(force)

	return ScanVerdict{
		FindingsByFile: findingsByFile,
		HasCritical:    hasCritical,
		ShouldBlock:    shouldBlock,
		CriticalCount:  counts[SeverityCritical],
		WarningCount:   counts[SeverityWarning],
		FilesScanned:   filesScanned,
	}
}
