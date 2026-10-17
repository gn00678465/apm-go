package deploy

import (
	"bytes"
	"os"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/lockfile"
)

// copyPrimitiveFile copies p's source to dst, as copiedBytes gives it.
func copyPrimitiveFile(p Primitive, dst string) error {
	data, err := os.ReadFile(p.SrcPath)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, copiedBytes(p, data), 0644)
}

// copiedBytes is what a copy of p's source data writes. An agent, an
// instruction, a prompt and a command are written through oracleText, or, by
// the copy a --global deploy makes when it cannot link, through
// globalCopyText; a hook is copied as it is
// (integration/base_integrator.py:266-270).
func copiedBytes(p Primitive, data []byte) []byte {
	switch p.Type {
	case TypeAgents, TypeInstructions, TypePrompts, TypeCommands:
	default:
		return data
	}
	if activeDeployMode.global {
		return globalCopyText(data)
	}
	return oracleText(data)
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
