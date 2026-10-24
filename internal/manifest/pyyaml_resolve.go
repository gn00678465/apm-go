package manifest

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// The plain scalars PyYAML's resolver reads as float, as an int that is not
// decimal, and as a timestamp (yaml/resolver.py:177-214).
var (
	pyyamlFloatRe     = regexp.MustCompile(`^(?:[-+]?[0-9][0-9_]*\.[0-9_]*(?:[eE][-+][0-9]+)?|\.[0-9][0-9_]*(?:[eE][-+][0-9]+)?|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)
	pyyamlOtherIntRe  = regexp.MustCompile(`^(?:[-+]?0b[0-1_]+|[-+]?0[0-7_]+|[-+]?0x[0-9a-fA-F_]+|[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+)$`)
	pyyamlTimestampRe = regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}|[0-9]{4}-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ \t]+)[0-9][0-9]?:[0-9]{2}:[0-9]{2}(?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9]{2})?))?)$`)
)

// PyYAMLResolve is PyYAML's implicit resolver for a plain scalar, in the
// order it tries the tags. A plain << is a merge key and a plain = the value
// tag, which the SafeLoader cannot construct.
func PyYAMLResolve(s string) string {
	switch {
	case PyYAMLBoolRe.MatchString(s):
		return "!!bool"
	case pyyamlFloatRe.MatchString(s):
		return "!!float"
	case PyYAMLDecimalIntRe.MatchString(s), pyyamlOtherIntRe.MatchString(s):
		return "!!int"
	case s == "<<":
		return "!!merge"
	case PyYAMLNullRe.MatchString(s):
		return "!!null"
	case pyyamlTimestampRe.MatchString(s):
		return "!!timestamp"
	case s == "=":
		return "!!value"
	}
	return "!!str"
}

// PyYAMLInt ports construct_yaml_int (yaml/constructor.py:237) for a text
// PyYAMLResolve takes as !!int. It fails for 0b_ and 0x_, which match the
// resolver and leave no digits.
func PyYAMLInt(text string) (*big.Int, error) {
	s := strings.ReplaceAll(text, "_", "")
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimLeft(s, "+-")
	v := new(big.Int)
	ok := true
	switch {
	case s == "0":
	case strings.HasPrefix(s, "0b"):
		_, ok = v.SetString(s[2:], 2)
	case strings.HasPrefix(s, "0x"):
		_, ok = v.SetString(s[2:], 16)
	case strings.HasPrefix(s, "0"):
		_, ok = v.SetString(s, 8)
	case strings.Contains(s, ":"):
		base := big.NewInt(1)
		parts := strings.Split(s, ":")
		for i := len(parts) - 1; i >= 0; i-- {
			d, _ := new(big.Int).SetString(parts[i], 10)
			v.Add(v, d.Mul(d, base))
			base.Mul(base, big.NewInt(60))
		}
	default:
		_, ok = v.SetString(s, 10)
	}
	if !ok {
		return nil, fmt.Errorf("invalid integer %q", text)
	}
	if neg {
		v.Neg(v)
	}
	return v, nil
}

// PyYAMLFloat ports construct_yaml_float (yaml/constructor.py:270) for a
// text PyYAMLResolve takes as !!float, so every part parses.
func PyYAMLFloat(text string) float64 {
	s := strings.ToLower(strings.ReplaceAll(text, "_", ""))
	sign := 1.0
	if strings.HasPrefix(s, "-") {
		sign = -1
	}
	s = strings.TrimLeft(s, "+-")
	switch {
	case s == ".inf":
		return sign * math.Inf(1)
	case s == ".nan":
		return math.NaN()
	case strings.Contains(s, ":"):
		value := 0.0
		base := big.NewInt(1)
		parts := strings.Split(s, ":")
		for i := len(parts) - 1; i >= 0; i-- {
			d, _ := strconv.ParseFloat(parts[i], 64)
			b, _ := new(big.Float).SetInt(base).Float64()
			value += d * b
			base.Mul(base, big.NewInt(60))
		}
		return sign * value
	}
	// Out of range is ±inf or 0, as Python's float() gives it.
	f, _ := strconv.ParseFloat(s, 64)
	return sign * f
}

// PyFloatRepr is CPython's float_repr: the shortest digits that round-trip,
// in exponent form when the decimal point is more than 16 places right of
// the first digit or 4 or more places left of it.
func PyFloatRepr(x float64) string {
	switch {
	case math.IsNaN(x):
		return "nan"
	case math.IsInf(x, 1):
		return "inf"
	case math.IsInf(x, -1):
		return "-inf"
	}
	sign := ""
	if math.Signbit(x) {
		sign = "-"
		x = -x
	}
	if x == 0 {
		return sign + "0.0"
	}
	mant, exp, _ := strings.Cut(strconv.FormatFloat(x, 'e', -1, 64), "e")
	digits := strings.Replace(mant, ".", "", 1)
	e, _ := strconv.Atoi(exp)
	decpt := e + 1
	if decpt > 16 || decpt < -3 {
		out := digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		esign := "+"
		if e < 0 {
			esign, e = "-", -e
		}
		return fmt.Sprintf("%s%se%s%02d", sign, out, esign, e)
	}
	switch {
	case decpt <= 0:
		return sign + "0." + strings.Repeat("0", -decpt) + digits
	case decpt >= len(digits):
		return sign + digits + strings.Repeat("0", decpt-len(digits)) + ".0"
	}
	return sign + digits[:decpt] + "." + digits[decpt:]
}

// PyYAMLFloatRepr is the text PyYAML's SafeRepresenter writes for a float
// (yaml/representer.py:171-189). It adds ".0" before the exponent of a
// repr with no decimal point, since its resolver reads 1e+20 as a string.
func PyYAMLFloatRepr(x float64) string {
	switch {
	case math.IsNaN(x):
		return ".nan"
	case math.IsInf(x, 1):
		return ".inf"
	case math.IsInf(x, -1):
		return "-.inf"
	}
	s := PyFloatRepr(x)
	if !strings.Contains(s, ".") && strings.Contains(s, "e") {
		s = strings.Replace(s, "e", ".0e", 1)
	}
	return s
}
