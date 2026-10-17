package deploy

import (
	"bytes"
	"os"
	"unicode/utf8"
)

// copyPrimitiveFile copies p's source to dst. An agent, an instruction, a
// prompt and a command are written with LF line endings, as the Oracle reads
// them in text mode and writes them with write_text_lf; a hook is copied as it
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
	return os.WriteFile(dst, lfText(data), 0644)
}

// lfText is content as the Oracle reads a primitive it writes back with
// write_text_lf: Path.read_text reads CRLF and a bare CR as LF
// (integration/agent_integrator.py:360-362, instruction_integrator.py:117-119,
// prompt_integrator.py:51-53, utils/atomic_io.py:28-48). Content that is not
// valid UTF-8, which the Oracle cannot read, is kept as it is.
func lfText(b []byte) []byte {
	if !utf8.Valid(b) {
		return b
	}
	return bytes.ReplaceAll(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), []byte("\r"), []byte("\n"))
}
