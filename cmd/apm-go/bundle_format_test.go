package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBundleFormatAliasesSingleDefinition guards ticket 04's shared-resolver
// contract: `init` and `pack` must not fork the alias table later. Walks
// every non-test .go file in this package and fails unless
// "bundleFormatAliases" is declared as a package-level var exactly once.
func TestBundleFormatAliasesSingleDefinition(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.VAR {
				continue
			}
			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, ident := range valueSpec.Names {
					if ident.Name == "bundleFormatAliases" {
						count++
					}
				}
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one bundleFormatAliases definition in cmd/apm-go, found %d", count)
	}
}

// TestResolveBundleFormat_PackChoices_ApmResolves proves the 5-choice list
// (packFormatChoices, ready for `pack`, ticket 07) resolves "apm" to mode
// "apm" through the shared resolver, while pluginFormatChoices (4-choice,
// see TestResolvePluginFormat) continues to reject it.
func TestResolveBundleFormat_PackChoices_ApmResolves(t *testing.T) {
	got, err := resolveBundleFormat("apm", true, false, packFormatChoices)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != bundleModeApm {
		t.Fatalf("mode = %q, want %q", got, bundleModeApm)
	}
}
