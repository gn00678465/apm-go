package deploy

import (
	"bytes"
	"os"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/lockfile"
)

// copyPrimitiveFile copies p's source to dst, the absolute path of destRel,
// as copiedBytes gives it.
func copyPrimitiveFile(p Primitive, destRel, dst string) error {
	data, err := os.ReadFile(p.SrcPath)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, copiedBytes(p, destRel, data), 0644)
}

// copiedBytes is what a copy of p's source data to destRel writes. An agent,
// an instruction, a prompt and a command are written through oracleText, or,
// by the copy a --global deploy makes when it cannot link, through
// globalCopyText; a hook is copied as it is
// (integration/base_integrator.py:266-270). An agent, an instruction and a
// prompt have their links rewritten (prompt_integrator.py:39-53,
// agent_integrator.py:348-362, instruction_integrator.py:177-202), but not
// at --global, where a link shows the source as it is (issue #126 comment
// 6267289737 item 1).
func copiedBytes(p Primitive, destRel string, data []byte) []byte {
	switch p.Type {
	case TypeAgents, TypeInstructions, TypePrompts, TypeCommands:
	default:
		return data
	}
	if activeDeployMode.global {
		return globalCopyText(data)
	}
	if copiedAsText(p.Type) {
		return rewriteLinks(p, destRel, oracleText(data))
	}
	return oracleText(data)
}

// comparedContent is what a file at destRel is compared with before p's
// source data is copied there: the source with each CRLF as LF, as the
// Oracle compares it, and the content written, links rewritten, when a link
// changes. The Oracle compares an instruction so
// (instruction_integrator.py:494-503) and an agent or a prompt with its
// source, which a frozen install, writing the rewritten content, could not
// restore once adopted (issue #126 comment 6270876048 item 1).
func comparedContent(p Primitive, destRel string, data []byte) []byte {
	if !activeDeployMode.global && copiedAsText(p.Type) {
		text := oracleText(data)
		if rewritten := rewriteLinks(p, destRel, text); !bytes.Equal(rewritten, text) {
			return rewritten
		}
	}
	return crlfToLF(data)
}

// skillSameOnDisk tells whether the skill directory abs, the copy of p at
// destDir, is what a skill there is compared with: the source as dircmp
// compares them, or, when the copy rewrites a link, what the copy writes
// (issue #126 comment 6270876048 item 1).
func skillSameOnDisk(p Primitive, destDir, abs string) bool {
	if skillRewritesLinks(p, destDir) {
		return skillCopyOnDisk(p, abs)
	}
	return skillDirsEqual(p.SrcPath, abs)
}

// skillRewritesLinks tells whether the copy of the skill p to destDir
// rewrites a link in one of its files.
func skillRewritesLinks(p Primitive, destDir string) bool {
	if activeDeployMode.global {
		return false
	}
	rewrites := false
	_ = walkSkillDir(p.SrcPath, skillLinksOf(p), skillCopyRulesOf(p), func(src, rel string, isDir bool) error {
		if isDir || rewrites {
			return nil
		}
		if data, err := os.ReadFile(src); err == nil {
			_, rewrites = rewriteSkillFile(p, destDir, rel, data)
		}
		return nil
	})
	return rewrites
}

// skillFileBytes is what the copy of the skill p writes at rel below destDir
// from its source file src: the file as it is, or a markdown file with its
// links rewritten (rewriteSkillFile), which --global leaves as it is (issue
// #126 comment 6267289737 item 1).
func skillFileBytes(p Primitive, destDir, rel, src string) ([]byte, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	if !activeDeployMode.global {
		if rewritten, ok := rewriteSkillFile(p, destDir, rel, data); ok {
			return rewritten, nil
		}
	}
	return data, nil
}

// oracleText is a primitive's content as the Oracle writes it: Path.read_text
// reads each CRLF and bare CR as a line end, NUL included, and write_text_lf
// writes LF (integration/agent_integrator.py:358-362, utils/atomic_io.py:
// 28-48). Content that is not UTF-8, which the Oracle cannot read, is kept as
// it is (issue #119 comment 6231670262).
func oracleText(b []byte) []byte {
	if !utf8.Valid(b) {
		return b
	}
	return bytes.ReplaceAll(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), []byte("\r"), []byte("\n"))
}

// globalCopyText is oracleText for the copy a --global deploy makes when it
// cannot link (issue #118): a link shows the source's bytes, and one lock has
// to restore under both (issue #118 comment 6229823230), so the copy changes
// only what the lock hash changes, each CRLF, and keeps content where that
// would leave a CRLF (a CR before a CRLF). A bare CR and content with a NUL
// stay as they are.
func globalCopyText(b []byte) []byte {
	lf := lockfile.CanonicalDeployedBytes(b)
	if bytes.Contains(lf, []byte("\r\n")) {
		return b
	}
	return lf
}
