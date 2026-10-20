package deploy

import (
	"os"
)

// IsAPMPackage is isAPMPackage for the package at dir.
func IsAPMPackage(dir string) bool {
	return isAPMPackage(sourceTree{dir: dir})
}

// SkillName is the name a validated Claude skill gets from the SKILL.md at
// path, models/validation.py:573: its frontmatter's name, else fallback.
// A name that is not a string is Python's str() of it, and a null one is
// none.
func SkillName(path, fallback string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	doc, err := readPromptDoc(content)
	if err != nil || doc.meta == nil {
		return fallback
	}
	v := doc.meta.get(pyStr("name"))
	switch v.(type) {
	case nil:
		return fallback
	case pyNone:
		return ""
	}
	return v.str()
}
