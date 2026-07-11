package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// StabilizeBuildID replaces the buildIDPlaceholder line with a
// deterministic 12-hex-char SHA256 prefix computed over the content with
// the placeholder line REMOVED (not blanked -- so the hash is never
// self-referential), the remaining lines LF-joined (design.md §5; oracle:
// compilation/build_id.py:22-39). Idempotent: content without the
// placeholder is returned unchanged. A trailing newline, if present, is
// preserved.
func StabilizeBuildID(content string) string {
	lines, hadTrailingNL := splitLinesLikePython(content)

	idx := -1
	for i, l := range lines {
		if l == buildIDPlaceholder {
			idx = i
			break
		}
	}
	if idx == -1 {
		return content
	}

	hashInput := make([]string, 0, len(lines)-1)
	for i, l := range lines {
		if i == idx {
			continue
		}
		hashInput = append(hashInput, l)
	}
	sum := sha256.Sum256([]byte(strings.Join(hashInput, "\n")))
	buildID := hex.EncodeToString(sum[:])[:12]
	lines[idx] = "<!-- Build ID: " + buildID + " -->"

	result := strings.Join(lines, "\n")
	if hadTrailingNL {
		result += "\n"
	}
	return result
}

// splitLinesLikePython mirrors Python's str.splitlines() applied to
// LF-only content: unlike strings.Split(content, "\n"), it does not
// produce a trailing empty element when content ends with "\n".
func splitLinesLikePython(content string) (lines []string, hadTrailingNL bool) {
	hadTrailingNL = strings.HasSuffix(content, "\n")
	lines = strings.Split(content, "\n")
	if hadTrailingNL && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	return lines, hadTrailingNL
}
