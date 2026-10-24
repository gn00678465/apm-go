package deploy

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/manifest"
)

// pyDump is what python-frontmatter writes for a metadata dict
// (default_handlers.py:262-271): yaml.dump(v, Dumper=CSafeDumper,
// default_flow_style=False, allow_unicode=True), stripped. That is PyYAML's
// SafeRepresenter with sorted keys (yaml/representer.py), emitted by libyaml
// 0.2.5 with indent 2 and width 80 (emitter.c). Only what a constructed
// document can reach is ported: block collections, empty ones in flow style,
// and the plain, single- and double-quoted styles libyaml picks for a scalar
// that asks for none.
func pyDump(v pyValue) string {
	e := &yamlEmitter{indent: -1, whitespace: true, indention: true}
	e.node(v, false, false)
	return pyStrip(e.out.String())
}

const (
	yamlBestIndent = 2
	yamlBestWidth  = 80
)

type yamlEmitter struct {
	out                   strings.Builder
	column, indent        int
	indents               []int
	whitespace, indention bool
	// mappingCtx and simpleKey are the context of the node being written.
	mappingCtx, simpleKey bool
}

func (e *yamlEmitter) increaseIndent(flow, indentless bool) {
	e.indents = append(e.indents, e.indent)
	switch {
	case e.indent < 0 && flow:
		e.indent = yamlBestIndent
	case e.indent < 0:
		e.indent = 0
	case !indentless:
		e.indent += yamlBestIndent
	}
}

func (e *yamlEmitter) popIndent() {
	e.indent = e.indents[len(e.indents)-1]
	e.indents = e.indents[:len(e.indents)-1]
}

func (e *yamlEmitter) put(s string) {
	e.out.WriteString(s)
	e.column += utf8.RuneCountInString(s)
}

func (e *yamlEmitter) putBreak() {
	e.out.WriteByte('\n')
	e.column = 0
}

func (e *yamlEmitter) writeIndent() {
	indent := max(e.indent, 0)
	if !e.indention || e.column > indent || (e.column == indent && !e.whitespace) {
		e.putBreak()
	}
	for e.column < indent {
		e.put(" ")
	}
	e.whitespace = true
	e.indention = true
}

func (e *yamlEmitter) writeIndicator(indicator string, needWhitespace, isWhitespace, isIndention bool) {
	if needWhitespace && !e.whitespace {
		e.put(" ")
	}
	e.put(indicator)
	e.whitespace = isWhitespace
	e.indention = e.indention && isIndention
}

// node is yaml_emitter_emit_node with the context flags the writing of v
// reads: whether it is in a mapping, and whether it is a simple key.
func (e *yamlEmitter) node(v pyValue, mapping, simpleKey bool) {
	e.mappingCtx, e.simpleKey = mapping, simpleKey
	switch x := v.(type) {
	case pyList:
		if len(x) == 0 {
			e.writeIndicator("[", true, true, false)
			e.writeIndicator("]", false, false, false)
			return
		}
		e.blockSequence(x)
	case *pyDict:
		if len(x.keys) == 0 {
			e.writeIndicator("{", true, true, false)
			e.writeIndicator("}", false, false, false)
			return
		}
		e.blockMapping(x)
	default:
		e.scalar(representScalar(v))
	}
}

func (e *yamlEmitter) blockSequence(items pyList) {
	e.increaseIndent(false, e.mappingCtx && !e.indention)
	for _, item := range items {
		e.writeIndent()
		e.writeIndicator("-", true, false, true)
		e.node(item, false, false)
	}
	e.popIndent()
}

func (e *yamlEmitter) blockMapping(d *pyDict) {
	e.increaseIndent(false, false)
	for _, i := range sortedKeyOrder(d) {
		e.writeIndent()
		key := representScalar(d.keys[i])
		if key.simpleKey() {
			e.node(d.keys[i], true, true)
			e.writeIndicator(":", false, false, false)
		} else {
			e.writeIndicator("?", true, false, true)
			e.node(d.keys[i], true, false)
			e.writeIndent()
			e.writeIndicator(":", true, false, true)
		}
		e.node(d.values[i], true, false)
	}
	e.popIndent()
}

// yamlScalar is a represented scalar: its text, and whether the resolver
// gives that text its tag when it is written plain.
type yamlScalar struct {
	value         string
	plainImplicit bool
}

// representScalar is SafeRepresenter's text for a scalar (represent_str,
// represent_bool, represent_int, represent_float, represent_none,
// represent_date, represent_datetime).
func representScalar(v pyValue) yamlScalar {
	switch x := v.(type) {
	case pyStr:
		return yamlScalar{string(x), resolvePlain(string(x)) == "!!str"}
	case pyBool:
		if x {
			return yamlScalar{"true", true}
		}
		return yamlScalar{"false", true}
	case pyNone:
		return yamlScalar{"null", true}
	case pyFloat:
		return yamlScalar{manifest.PyYAMLFloatRepr(float64(x)), true}
	}
	// int, date and datetime: str() is the represented text.
	return yamlScalar{v.str(), true}
}

// simpleKey is yaml_emitter_check_simple_key for a scalar key.
func (s yamlScalar) simpleKey() bool {
	return !analyzeYAMLScalar(s.value).multiline && len(s.value) <= 128
}

func (e *yamlEmitter) scalar(s yamlScalar) {
	a := analyzeYAMLScalar(s.value)
	style := byte('p')
	if e.simpleKey && a.multiline {
		style = '"'
	}
	if style == 'p' && (!a.blockPlainAllowed || (s.value == "" && e.simpleKey) || !s.plainImplicit) {
		style = '\''
	}
	if style == '\'' && !a.singleQuotedAllowed {
		style = '"'
	}
	e.increaseIndent(true, false)
	allowBreaks := !e.simpleKey
	switch style {
	case 'p':
		e.writePlain(s.value, allowBreaks)
	case '\'':
		e.writeSingleQuoted(s.value, allowBreaks)
	default:
		e.writeDoubleQuoted(s.value, allowBreaks)
	}
	e.popIndent()
}

type yamlScalarAnalysis struct {
	multiline, blockPlainAllowed, singleQuotedAllowed bool
}

// analyzeYAMLScalar is yaml_emitter_analyze_scalar, with unicode on, for the
// styles a block context can pick.
func analyzeYAMLScalar(value string) yamlScalarAnalysis {
	if value == "" {
		return yamlScalarAnalysis{blockPlainAllowed: true, singleQuotedAllowed: true}
	}
	var blockIndicators, lineBreaks, special, leadingSpace, leadingBreak,
		trailingSpace, trailingBreak, breakSpace, spaceBreak, previousSpace, previousBreak bool
	if strings.HasPrefix(value, "---") || strings.HasPrefix(value, "...") {
		blockIndicators = true
	}
	runes := []rune(value)
	precededByWhitespace := true
	for i, r := range runes {
		followedByWhitespace := i+1 == len(runes) || yamlBlankz(runes[i+1])
		if i == 0 {
			switch r {
			case '#', ',', '[', ']', '{', '}', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`':
				blockIndicators = true
			case '?', ':', '-':
				if followedByWhitespace {
					blockIndicators = true
				}
			}
		} else {
			switch r {
			case ':':
				if followedByWhitespace {
					blockIndicators = true
				}
			case '#':
				if precededByWhitespace {
					blockIndicators = true
				}
			}
		}
		if !yamlPrintable(r) {
			special = true
		}
		switch {
		case r == ' ':
			leadingSpace = leadingSpace || i == 0
			trailingSpace = trailingSpace || i == len(runes)-1
			breakSpace = breakSpace || previousBreak
			previousSpace, previousBreak = true, false
		case yamlBreak(r):
			lineBreaks = true
			leadingBreak = leadingBreak || i == 0
			trailingBreak = trailingBreak || i == len(runes)-1
			spaceBreak = spaceBreak || previousSpace
			previousSpace, previousBreak = false, true
		default:
			previousSpace, previousBreak = false, false
		}
		precededByWhitespace = yamlBlankz(r)
	}
	a := yamlScalarAnalysis{multiline: lineBreaks, blockPlainAllowed: true, singleQuotedAllowed: true}
	if leadingSpace || leadingBreak || trailingSpace || trailingBreak || lineBreaks || blockIndicators {
		a.blockPlainAllowed = false
	}
	if breakSpace || spaceBreak || special {
		a.blockPlainAllowed = false
		a.singleQuotedAllowed = false
	}
	return a
}

// yamlPrintable is libyaml's IS_PRINTABLE: tab, CR, NEL, the C1 controls,
// the BOM, U+FFFE, U+FFFF and every character beyond the BMP are not.
func yamlPrintable(r rune) bool {
	switch {
	case r == '\n':
		return true
	case r >= 0x20 && r <= 0x7e:
		return true
	case r >= 0xa0 && r <= 0xd7ff:
		return true
	case r >= 0xe000 && r <= 0xfffd:
		return r != 0xfeff
	}
	return false
}

func yamlBreak(r rune) bool {
	return r == '\n' || r == '\r' || r == 0x85 || r == 0x2028 || r == 0x2029
}

func yamlBlankz(r rune) bool {
	return r == ' ' || r == '\t' || r == 0 || yamlBreak(r)
}

func (e *yamlEmitter) writePlain(value string, allowBreaks bool) {
	if !e.whitespace && value != "" {
		e.put(" ")
	}
	runes := []rune(value)
	spaces := false
	for i, r := range runes {
		if r == ' ' {
			if allowBreaks && !spaces && e.column > yamlBestWidth && (i+1 == len(runes) || runes[i+1] != ' ') {
				e.writeIndent()
			} else {
				e.put(" ")
			}
			spaces = true
			continue
		}
		e.put(string(r))
		e.indention = false
		spaces = false
	}
	e.whitespace = false
	e.indention = false
}

func (e *yamlEmitter) writeSingleQuoted(value string, allowBreaks bool) {
	e.writeIndicator("'", true, false, false)
	runes := []rune(value)
	spaces, breaks := false, false
	for i, r := range runes {
		switch {
		case r == ' ':
			if allowBreaks && !spaces && e.column > yamlBestWidth && i != 0 && i != len(runes)-1 && runes[i+1] != ' ' {
				e.writeIndent()
			} else {
				e.put(" ")
			}
			spaces = true
		case yamlBreak(r):
			if !breaks && r == '\n' {
				e.putBreak()
			}
			e.writeBreak(r)
			e.indention = true
			breaks = true
		default:
			if breaks {
				e.writeIndent()
			}
			if r == '\'' {
				e.put("'")
			}
			e.put(string(r))
			e.indention = false
			spaces, breaks = false, false
		}
	}
	if breaks {
		e.writeIndent()
	}
	e.writeIndicator("'", false, false, false)
	e.whitespace = false
	e.indention = false
}

// writeBreak is WRITE_BREAK: a line feed is the emitter's own break, any
// other break character is copied.
func (e *yamlEmitter) writeBreak(r rune) {
	if r == '\n' {
		e.putBreak()
		return
	}
	e.out.WriteRune(r)
	e.column = 0
}

var yamlEscapes = map[rune]byte{
	0x00: '0', 0x07: 'a', 0x08: 'b', 0x09: 't', 0x0a: 'n', 0x0b: 'v', 0x0c: 'f', 0x0d: 'r',
	0x1b: 'e', '"': '"', '\\': '\\', 0x85: 'N', 0xa0: '_', 0x2028: 'L', 0x2029: 'P',
}

func (e *yamlEmitter) writeDoubleQuoted(value string, allowBreaks bool) {
	e.writeIndicator(`"`, true, false, false)
	runes := []rune(value)
	spaces := false
	for i, r := range runes {
		switch {
		case !yamlPrintable(r) || r == 0xfeff || yamlBreak(r) || r == '"' || r == '\\':
			if c, ok := yamlEscapes[r]; ok {
				e.put(`\` + string(c))
			} else {
				switch {
				case r <= 0xff:
					e.put(fmt.Sprintf(`\x%02X`, r))
				case r <= 0xffff:
					e.put(fmt.Sprintf(`\u%04X`, r))
				default:
					e.put(fmt.Sprintf(`\U%08X`, r))
				}
			}
			spaces = false
		case r == ' ':
			if allowBreaks && !spaces && e.column > yamlBestWidth && i != 0 && i != len(runes)-1 {
				e.writeIndent()
				if runes[i+1] == ' ' {
					e.put(`\`)
				}
			} else {
				e.put(" ")
			}
			spaces = true
		default:
			e.put(string(r))
			spaces = false
		}
	}
	e.writeIndicator(`"`, false, false, false)
	e.whitespace = false
	e.indention = false
}

// sortedKeyOrder is the order SafeRepresenter writes a dict's keys in:
// sorted(mapping.items()), or the dict's own order when Python cannot
// compare two of its keys (a TypeError the representer swallows).
func sortedKeyOrder(d *pyDict) []int {
	order := make([]int, len(d.keys))
	for i := range order {
		order[i] = i
	}
	class := pyOrderClass(d.keys[0])
	for _, k := range d.keys[1:] {
		if c := pyOrderClass(k); c == "" || c != class {
			return order
		}
	}
	if len(d.keys) > 1 && class == "" {
		return order
	}
	sort.SliceStable(order, func(i, j int) bool { return pyLess(d.keys[order[i]], d.keys[order[j]]) })
	return order
}

// pyOrderClass names the keys Python's < orders among themselves; "" is a
// key it orders with no other (None, NaN).
func pyOrderClass(v pyValue) string {
	switch x := v.(type) {
	case pyStr:
		return "str"
	case pyBool, pyInt:
		return "number"
	case pyFloat:
		if math.IsNaN(float64(x)) {
			return ""
		}
		return "number"
	case pyDate:
		return "date"
	case pyDatetime:
		if x.tz == nil {
			return "naive datetime"
		}
		return "aware datetime"
	}
	return ""
}

func pyLess(a, b pyValue) bool {
	switch x := a.(type) {
	case pyStr:
		return x < b.(pyStr)
	case pyDate:
		y := b.(pyDate)
		return x.year < y.year || x.year == y.year && (x.month < y.month || x.month == y.month && x.day < y.day)
	case pyDatetime:
		return x.instant() < b.(pyDatetime).instant()
	}
	fa, fb := pyFloatOf(a), pyFloatOf(b)
	if math.IsInf(fa, 0) || math.IsInf(fb, 0) {
		return fa < fb
	}
	ra, _ := pyNumber(a)
	rb, _ := pyNumber(b)
	return ra.Cmp(rb) < 0
}

func pyFloatOf(v pyValue) float64 {
	if f, ok := v.(pyFloat); ok {
		return float64(f)
	}
	return 0
}
