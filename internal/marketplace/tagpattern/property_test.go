package tagpattern

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"testing/quick"
)

// Property: for every supported pattern shape, a tag rendered from a
// version must extract back to exactly that version (render/extract
// round trip), and Validate must accept the pattern.
func TestRenderExtractRoundTripProperty(t *testing.T) {
	patterns := []string{"v{version}", "{version}", "{name}-v{version}", "release/{version}", "{name}@{version}"}
	names := []string{"pkg", "my-tool", "a1"}
	f := func(pi, ni uint8, major, minor, patch uint16) bool {
		p := patterns[int(pi)%len(patterns)]
		name := names[int(ni)%len(names)]
		version := strconv.Itoa(int(major)) + "." + strconv.Itoa(int(minor)) + "." + strconv.Itoa(int(patch))
		if _, err := Validate(p, "packages[0].tag_pattern"); err != nil {
			return false
		}
		tag := RenderTag(p, name, version)
		got, ok := ExtractVersion(Compile(p, name), tag)
		return ok && got == version
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500, Rand: rand.New(rand.NewSource(1))}); err != nil {
		t.Fatal(err)
	}
}

// Property: a pattern with any placeholder count other than exactly one
// {version}, or any placeholder other than {version}/{name}, is rejected.
func TestValidateRejectsMalformedProperty(t *testing.T) {
	f := func(n uint8, extra bool) bool {
		count := int(n) % 4
		p := "v" + strings.Repeat("{version}", count)
		if extra {
			p = "{other}" + p
		}
		_, err := Validate(p, "ctx")
		wantOK := count == 1 && !extra
		return (err == nil) == wantOK
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200, Rand: rand.New(rand.NewSource(2))}); err != nil {
		t.Fatal(err)
	}
}
