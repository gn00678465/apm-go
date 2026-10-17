package deploy

import (
	"bytes"
	"os"

	"github.com/apm-go/apm/internal/lockfile"
)

// copyPrimitiveFile copies p's source to dst. An agent, an instruction, a
// prompt and a command are written through deployText; a hook is copied as it
// is (integration/base_integrator.py:266-270).
func copyPrimitiveFile(p Primitive, dst string) error {
	switch p.Type {
	case TypeAgents, TypeInstructions, TypePrompts, TypeCommands:
	default:
		return copyFile(p.SrcPath, dst)
	}
	data, err := os.ReadFile(p.SrcPath)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, deployText(data), 0644)
}

// deployText is a primitive's content with each CRLF written as LF, by the
// conversion lockfile.HashDeployedFile hashes, so a copy hashes as the source
// a --global symlink shows; one lock has to restore under a copy deploy and a
// symlink deploy alike (issue #118 comment 6229823230). Content that this
// conversion would leave with a CRLF (a CR before a CRLF) is kept as it is, as
// are a bare CR, content with a NUL and content that is not UTF-8. The Oracle
// writes the first three as LF (Path.read_text, then write_text_lf,
// utils/atomic_io.py:28-48); its user scope only copies.
func deployText(b []byte) []byte {
	lf := lockfile.CanonicalDeployedBytes(b)
	if bytes.Contains(lf, []byte("\r\n")) {
		return b
	}
	return lf
}
