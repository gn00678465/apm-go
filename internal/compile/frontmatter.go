package compile

import (
	"strings"

	"github.com/apm-go/apm/internal/deploy"
)

// ParsedInstruction is one *.instructions.md file's parsed frontmatter +
// body (oracle: primitives/parser.py:80-119).
type ParsedInstruction struct {
	// ApplyTo is the normalized applyTo the Oracle groups by
	// (normalize_apply_to): a list is its items joined with ",", a comma
	// inside an item escaped. It is not split, unlike install's
	// .claude/rules paths. Empty means "no applyTo" (global).
	ApplyTo string
	// Body is the content after the frontmatter block, stripped of
	// leading/trailing whitespace (oracle: instruction.content.strip()).
	Body string
}

// ParseInstruction parses one instruction file's raw bytes. A frontmatter
// the Oracle cannot read is an error.
func ParseInstruction(content []byte) (ParsedInstruction, error) {
	applyTo, body, err := deploy.ReadInstruction(content)
	if err != nil {
		return ParsedInstruction{}, err
	}
	return ParsedInstruction{ApplyTo: applyTo, Body: strings.TrimSpace(body)}, nil
}
