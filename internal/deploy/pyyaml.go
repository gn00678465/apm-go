package deploy

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
)

// pyValue is a YAML node as the Oracle's loader (PyYAML's SafeLoader,
// YAML 1.1) constructs it. str and repr give the text of Python's str() and
// repr() of that object, which is what the Oracle writes for a non-string
// applyTo.
type pyValue interface {
	str() string
	repr() string
}

type (
	pyNone  struct{}
	pyBool  bool
	pyInt   struct{ v *big.Int }
	pyFloat float64
	pyStr   string
	pyDate  struct{ year, month, day int }
	// pyDatetime has tz nil when it is naive, else its UTC offset in seconds.
	pyDatetime struct {
		date                         pyDate
		hour, minute, second, micros int
		tz                           *int
	}
	pyList []pyValue
	pyDict struct{ keys, values []pyValue }
)

var (
	// pyyamlTimestampParts is the constructor's regex (yaml/constructor.py:310).
	pyyamlTimestampParts = regexp.MustCompile(`^([0-9]{4})-([0-9][0-9]?)-([0-9][0-9]?)(?:(?:[Tt]|[ \t]+)([0-9][0-9]?):([0-9]{2}):([0-9]{2})(?:\.([0-9]*))?(?:[ \t]*(Z|([-+])([0-9][0-9]?)(?::([0-9]{2}))?))?)?$`)
)

// pyMaxIntDigits is CPython's int_max_str_digits: a longer decimal int
// cannot be converted to or from a string, so the Oracle fails to read it.
const pyMaxIntDigits = 4300

// constructPy builds the object PyYAML's SafeLoader makes of a document
// yamlcore.SafeLoad accepted. Where the SafeLoader raises, so does this.
func constructPy(n *yaml.Node) (pyValue, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return pyNone{}, nil
		}
		return constructPy(n.Content[0])
	case yaml.SequenceNode:
		if err := checkCollectionTag(n, "!!seq"); err != nil {
			return nil, err
		}
		list := pyList{}
		for _, c := range n.Content {
			v, err := constructPy(c)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		return list, nil
	case yaml.MappingNode:
		if err := checkCollectionTag(n, "!!map"); err != nil {
			return nil, err
		}
		return constructPyDict(n)
	case yaml.ScalarNode:
		return constructPyScalar(n)
	}
	return nil, fmt.Errorf("line %d: unsupported YAML node", n.Line)
}

func checkCollectionTag(n *yaml.Node, tag string) error {
	if n.Style&yaml.TaggedStyle != 0 && n.ShortTag() != tag {
		return fmt.Errorf("line %d: YAML tag %s is not supported", n.Line, n.ShortTag())
	}
	return nil
}

// constructPyDict keeps the first key and the last value of keys Python
// holds equal (1, 1.0 and true are one key), as a dict assignment does.
func constructPyDict(n *yaml.Node) (pyValue, error) {
	d := &pyDict{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].ShortTag() == "!!merge" {
			return nil, fmt.Errorf("line %d: YAML merge keys are not allowed", n.Content[i].Line)
		}
		k, err := constructPy(n.Content[i])
		if err != nil {
			return nil, err
		}
		switch k.(type) {
		case pyList, *pyDict:
			return nil, fmt.Errorf("line %d: found unhashable key", n.Content[i].Line)
		}
		v, err := constructPy(n.Content[i+1])
		if err != nil {
			return nil, err
		}
		d.set(k, v)
	}
	return d, nil
}

func (d *pyDict) set(k, v pyValue) {
	for i, existing := range d.keys {
		if pyKeyEqual(existing, k) {
			d.values[i] = v
			return
		}
	}
	d.keys = append(d.keys, k)
	d.values = append(d.values, v)
}

func (d *pyDict) get(k pyValue) pyValue {
	for i, existing := range d.keys {
		if pyKeyEqual(existing, k) {
			return d.values[i]
		}
	}
	return nil
}

// pyKeyEqual is Python's == on hashable keys: bool, int and float compare
// as numbers, an aware datetime by its instant.
func pyKeyEqual(a, b pyValue) bool {
	if x, ok := pyNumber(a); ok {
		y, ok := pyNumber(b)
		return ok && x.Cmp(y) == 0
	}
	switch x := a.(type) {
	case pyFloat:
		// An infinity or NaN. PyYAML gives every .nan one float object,
		// which a dict matches by identity, so NaN keys are one key too.
		y, ok := b.(pyFloat)
		return ok && (x == y || math.IsNaN(float64(x)) && math.IsNaN(float64(y)))
	case pyNone:
		_, ok := b.(pyNone)
		return ok
	case pyStr:
		y, ok := b.(pyStr)
		return ok && x == y
	case pyDate:
		y, ok := b.(pyDate)
		return ok && x == y
	case pyDatetime:
		y, ok := b.(pyDatetime)
		if !ok || (x.tz == nil) != (y.tz == nil) {
			return false
		}
		return x.instant() == y.instant()
	}
	return false
}

// pyNumber gives a bool, int or finite float as an exact number. An
// infinity and NaN equal no such number, so they have none.
func pyNumber(v pyValue) (*big.Rat, bool) {
	switch x := v.(type) {
	case pyBool:
		if x {
			return big.NewRat(1, 1), true
		}
		return new(big.Rat), true
	case pyInt:
		return new(big.Rat).SetInt(x.v), true
	case pyFloat:
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return nil, false
		}
		return new(big.Rat).SetFloat64(float64(x)), true
	}
	return nil, false
}

func constructPyScalar(n *yaml.Node) (pyValue, error) {
	tag := ""
	switch {
	case n.Style&yaml.TaggedStyle != 0:
		tag = n.ShortTag()
	case n.Style&^yaml.FlowStyle != 0:
		// Quoted, literal or folded: always a string.
		return pyStr(n.Value), nil
	default:
		tag = resolvePlain(n.Value)
	}
	if tag != "!!str" && tag != "!!null" && resolvePlain(n.Value) != tag {
		// An explicit tag the resolver would not give this text: the
		// SafeLoader converts it its own way or raises.
		return nil, fmt.Errorf("line %d: YAML tag %s on %q is not supported", n.Line, tag, n.Value)
	}
	switch tag {
	case "!!str":
		return pyStr(n.Value), nil
	case "!!null":
		return pyNone{}, nil
	case "!!bool":
		switch strings.ToLower(n.Value) {
		case "yes", "true", "on":
			return pyBool(true), nil
		}
		return pyBool(false), nil
	case "!!int":
		return constructPyInt(n)
	case "!!float":
		return constructPyFloat(n.Value), nil
	case "!!timestamp":
		return constructPyTimestamp(n)
	}
	return nil, fmt.Errorf("line %d: YAML tag %s on %q is not supported", n.Line, tag, n.Value)
}

// resolvePlain is PyYAML's implicit resolver for a plain scalar.
func resolvePlain(s string) string { return manifest.PyYAMLResolve(s) }

// constructPyInt ports construct_yaml_int (yaml/constructor.py:237).
func constructPyInt(n *yaml.Node) (pyValue, error) {
	v, err := manifest.PyYAMLInt(n.Value)
	if err != nil {
		return nil, fmt.Errorf("line %d: %w", n.Line, err)
	}
	if len(new(big.Int).Abs(v).String()) > pyMaxIntDigits {
		return nil, fmt.Errorf("line %d: integer exceeds %d digits", n.Line, pyMaxIntDigits)
	}
	return pyInt{v}, nil
}

// constructPyFloat ports construct_yaml_float (yaml/constructor.py:270).
func constructPyFloat(text string) pyValue { return pyFloat(manifest.PyYAMLFloat(text)) }

// constructPyTimestamp ports construct_yaml_timestamp
// (yaml/constructor.py:322) with the range checks of Python's date,
// datetime and timezone.
func constructPyTimestamp(n *yaml.Node) (pyValue, error) {
	m := pyyamlTimestampParts.FindStringSubmatch(n.Value)
	if m == nil {
		return nil, fmt.Errorf("line %d: invalid timestamp %q", n.Line, n.Value)
	}
	atoi := func(s string) int { v, _ := strconv.Atoi(s); return v }
	date := pyDate{atoi(m[1]), atoi(m[2]), atoi(m[3])}
	if date.year < 1 || date.month < 1 || date.month > 12 || date.day < 1 || date.day > daysIn(date.year, date.month) {
		return nil, fmt.Errorf("line %d: invalid date %q", n.Line, n.Value)
	}
	if m[4] == "" {
		return date, nil
	}
	dt := pyDatetime{date: date, hour: atoi(m[4]), minute: atoi(m[5]), second: atoi(m[6])}
	if dt.hour > 23 || dt.minute > 59 || dt.second > 59 {
		return nil, fmt.Errorf("line %d: invalid time %q", n.Line, n.Value)
	}
	if f := m[7]; f != "" {
		if len(f) > 6 {
			f = f[:6]
		}
		dt.micros = atoi(f + strings.Repeat("0", 6-len(f)))
	}
	switch {
	case m[9] != "":
		offset := atoi(m[10])*3600 + atoi(m[11])*60
		if m[9] == "-" {
			offset = -offset
		}
		if offset <= -86400 || offset >= 86400 {
			return nil, fmt.Errorf("line %d: invalid UTC offset %q", n.Line, n.Value)
		}
		dt.tz = &offset
	case m[8] == "Z":
		utc := 0
		dt.tz = &utc
	}
	return dt, nil
}

func daysIn(year, month int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// instant orders datetimes of one kind: microseconds since 1970, of the
// UTC time for an aware one.
func (d pyDatetime) instant() int64 {
	t := time.Date(d.date.year, time.Month(d.date.month), d.date.day, d.hour, d.minute, d.second, d.micros*1000, time.UTC)
	us := t.UnixMicro()
	if d.tz != nil {
		us -= int64(*d.tz) * 1_000_000
	}
	return us
}

func (pyNone) str() string  { return "None" }
func (pyNone) repr() string { return "None" }

func (b pyBool) str() string { return b.repr() }
func (b pyBool) repr() string {
	if b {
		return "True"
	}
	return "False"
}

func (i pyInt) str() string  { return i.v.String() }
func (i pyInt) repr() string { return i.v.String() }

func (f pyFloat) str() string { return f.repr() }

// repr is CPython's float_repr.
func (f pyFloat) repr() string { return manifest.PyFloatRepr(float64(f)) }

func (s pyStr) str() string { return string(s) }

// repr is CPython's unicode_repr.
func (s pyStr) repr() string {
	quote := '\''
	if strings.ContainsRune(string(s), '\'') && !strings.ContainsRune(string(s), '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range string(s) {
		switch {
		case r == quote || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < ' ' || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x7f || unicode.IsPrint(r):
			b.WriteRune(r)
		case r <= 0xff:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteRune(quote)
	return b.String()
}

func (d pyDate) str() string { return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day) }
func (d pyDate) repr() string {
	return fmt.Sprintf("datetime.date(%d, %d, %d)", d.year, d.month, d.day)
}

func (d pyDatetime) str() string {
	s := fmt.Sprintf("%s %02d:%02d:%02d", d.date.str(), d.hour, d.minute, d.second)
	if d.micros != 0 {
		s += fmt.Sprintf(".%06d", d.micros)
	}
	if d.tz != nil {
		off, sign := *d.tz, "+"
		if off < 0 {
			off, sign = -off, "-"
		}
		s += fmt.Sprintf("%s%02d:%02d", sign, off/3600, off%3600/60)
	}
	return s
}

func (d pyDatetime) repr() string {
	s := fmt.Sprintf("datetime.datetime(%d, %d, %d, %d, %d", d.date.year, d.date.month, d.date.day, d.hour, d.minute)
	switch {
	case d.micros != 0:
		s += fmt.Sprintf(", %d, %d", d.second, d.micros)
	case d.second != 0:
		s += fmt.Sprintf(", %d", d.second)
	}
	switch {
	case d.tz == nil:
	case *d.tz == 0:
		s += ", tzinfo=datetime.timezone.utc"
	default:
		// timedelta normalizes to whole days and 0 <= seconds < 86400.
		days, secs := 0, *d.tz
		if secs < 0 {
			days, secs = -1, secs+86400
		}
		td := fmt.Sprintf("seconds=%d", secs)
		if days != 0 {
			td = fmt.Sprintf("days=%d, %s", days, td)
		}
		s += fmt.Sprintf(", tzinfo=datetime.timezone(datetime.timedelta(%s))", td)
	}
	return s + ")"
}

func (l pyList) str() string { return l.repr() }
func (l pyList) repr() string {
	items := make([]string, len(l))
	for i, v := range l {
		items[i] = v.repr()
	}
	return "[" + strings.Join(items, ", ") + "]"
}

func (d *pyDict) str() string { return d.repr() }
func (d *pyDict) repr() string {
	items := make([]string, len(d.keys))
	for i := range d.keys {
		items[i] = d.keys[i].repr() + ": " + d.values[i].repr()
	}
	return "{" + strings.Join(items, ", ") + "}"
}
