package bundle

import (
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

func TestSynthesize_MissingName_Errors(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("version: 1.0.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Synthesize(doc.Content[0])
	if err == nil {
		t.Fatal("expected an error for a missing name field")
	}
}

func TestSynthesize_EmptyName_Errors(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("name: \"\"\nversion: 1.0.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Synthesize(doc.Content[0])
	if err == nil {
		t.Fatal("expected an error for an empty name field")
	}
}

func TestSynthesize_MinimalFields(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("name: demo\nversion: 1.0.0\ndescription: A demo\nlicense: MIT\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Synthesize(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "demo" || m.Version != "1.0.0" || m.Description != "A demo" || m.License != "MIT" {
		t.Errorf("m = %+v", m)
	}
	if m.Author != nil {
		t.Errorf("Author = %+v, want nil (no author: key)", m.Author)
	}
}

func TestSynthesize_AuthorString_BecomesNameOnly(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("name: demo\nauthor: Jane Doe\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Synthesize(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	if m.Author == nil || m.Author.Name != "Jane Doe" || m.Author.Email != "" || m.Author.URL != "" {
		t.Errorf("Author = %+v, want {Name: Jane Doe}", m.Author)
	}
}

func TestSynthesize_AuthorDict_KeepsRecognizedKeys(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("name: demo\nauthor:\n  name: Jane Doe\n  email: jane@example.com\n  url: https://example.com\n  extra: ignored\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Synthesize(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	want := Author{Name: "Jane Doe", Email: "jane@example.com", URL: "https://example.com"}
	if m.Author == nil || *m.Author != want {
		t.Errorf("Author = %+v, want %+v", m.Author, want)
	}
}

func TestSynthesize_AuthorDict_MissingName_DropsWholeField(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("name: demo\nauthor:\n  email: jane@example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Synthesize(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	if m.Author != nil {
		t.Errorf("Author = %+v, want nil (name missing from author dict)", m.Author)
	}
}

func TestSynthesize_HomepageAndRepository(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("name: demo\nhomepage: https://example.com\nrepository: https://github.com/acme/demo\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Synthesize(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	if m.Homepage != "https://example.com" || m.Repository != "https://github.com/acme/demo" {
		t.Errorf("m = %+v", m)
	}
}

func TestSynthesize_KeywordsSingleString_WrappedInList(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("name: demo\nkeywords: solo\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Synthesize(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Keywords) != 1 || m.Keywords[0] != "solo" {
		t.Errorf("Keywords = %v, want [solo]", m.Keywords)
	}
}

func TestSynthesize_KeywordsList(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("name: demo\nkeywords: [a, b, c]\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Synthesize(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Keywords) != 3 || m.Keywords[0] != "a" || m.Keywords[2] != "c" {
		t.Errorf("Keywords = %v, want [a b c]", m.Keywords)
	}
}
