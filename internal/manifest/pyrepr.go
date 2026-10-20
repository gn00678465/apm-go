package manifest

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PyRepr is Python's repr() of a str, an invalid UTF-8 byte b being the lone
// surrogate U+DC00+b Python decodes a path to. It follows the rule of
// pythonReprPyStr (internal/marketplace/models.go), which imports this
// package.
func PyRepr(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var sb strings.Builder
	sb.WriteRune(quote)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&sb, `\u%04x`, 0xdc00+int(s[i]))
		case r == quote || r == '\\':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r == '\t':
			sb.WriteString(`\t`)
		case unicode.IsPrint(r):
			sb.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&sb, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&sb, `\u%04x`, r)
		default:
			fmt.Fprintf(&sb, `\U%08x`, r)
		}
		i += size
	}
	sb.WriteRune(quote)
	return sb.String()
}
