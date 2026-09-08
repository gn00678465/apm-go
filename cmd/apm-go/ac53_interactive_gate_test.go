package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// interactiveImportPrefixes lists import paths this project treats as
// "interactive-capable" for AC53's purposes (D13: `marketplace init` must
// stay non-interactive): charm.land/huh (the TUI form library apm-go's own
// ux.Clack wraps) and this project's own internal/ux package.
//
// The two are NOT treated identically below: huh's entire exported surface
// is interactive form primitives (NewForm, group/field builders, etc.) --
// there is no legitimate non-interactive reason for marketplaceInitCmd to
// reference it at all, so ANY call through a bound huh identifier is a
// violation. internal/ux, in contrast, also exports plain, non-interactive
// output helpers marketplaceInitCmd genuinely uses today (ux.Success,
// ux.BulletList, ux.Item, ux.Section) -- banning every ux.* call would be a
// false positive, so only the SPECIFIC interactive selectors
// (interactiveUXSelectors below) are checked for a bound ux identifier,
// mirroring verify.ps1's own prior denylist exactly.
var interactiveImportPrefixes = []string{
	"charm.land/huh",
	"github.com/apm-go/apm/internal/ux",
}

// interactiveDefaultIdent maps each prefix above to the identifier Go code
// binds it to when the import has no explicit alias -- i.e. the package's
// own declared `package NAME` clause. verify.ps1's prior regex-based check
// already assumed "huh" for charm.land/huh/vN; this preserves that same
// assumption for the AST-based replacement, and adds "ux" for internal/ux by
// the same convention.
var interactiveDefaultIdent = map[string]string{
	"charm.land/huh":                    "huh",
	"github.com/apm-go/apm/internal/ux": "ux",
}

// interactiveUXSelectors is the exact set of internal/ux selectors
// verify.ps1's prior regex-based denylist banned (ux.NewClack, ux.InputText,
// ux.Password, ux.MultiSelect, ux.InputForm, ux.Confirm) -- ux's other
// exports (Success, BulletList, Item, Section, ...) are plain, non-blocking
// output helpers marketplaceInitCmd is expected to keep using.
var interactiveUXSelectors = map[string]bool{
	"NewClack":    true,
	"InputText":   true,
	"Password":    true,
	"MultiSelect": true,
	"InputForm":   true,
	"Confirm":     true,
}

// interactiveClackVarSelectors covers apm-go's own convention (init.go, this
// package: `var ck *ux.Clack`) of naming a *ux.Clack instance "ck" -- a LOCAL
// VARIABLE, not an import alias, so it cannot be discovered from the import
// declarations at all. This is the exact selector set verify.ps1's prior
// denylist banned for "ck." (ck's other methods -- Bar, Detail, Step, Warn
// -- are plain transcript decoration, not interactive prompts or the
// interactive-only parts of the clack transcript style).
var interactiveClackVarSelectors = map[string]bool{
	"Form":        true,
	"MultiSelect": true,
	"Confirm":     true,
	"Banner":      true,
	"Intro":       true,
	"Outro":       true,
}

// interactiveClackVarNames is the set of local-variable names checked
// against interactiveClackVarSelectors above.
var interactiveClackVarNames = map[string]bool{
	"ck": true,
}

// callable is one package-level thing this gate can enter and scan: either a
// plain top-level func declaration, or a package-level var whose initializer
// is a func literal -- both have an *ast.BlockStmt body. binds is the OWNING
// FILE's own import-identifier bindings (see fileBindings), since two files
// in the same package can bind the same identifier to different imports (or
// none at all).
type ac53Callable struct {
	body  *ast.BlockStmt
	binds map[string]string
}

// ac53Alias is a package-level var whose initializer is DIRECTLY a bound
// identifier's selector expression (`var x = ux.Confirm`) -- i.e. calling the
// var is equivalent to calling that selector directly, with no function body
// of its own to recurse into.
type ac53Alias struct {
	prefix   string // "" for a non-interactive-prefix selector (still tracked so an alias-of-an-alias chain can resolve through it)
	selector string
}

// TestMarketplaceInitCmd_NoInteractiveComponents is AC53's authoritative
// non-interactivity gate. verify.ps1's original regex-based denylist
// (`(?:(\w+)\s+)?"charm\.land/huh...`) resolved a huh import's bound
// identifier via `\w+`, which can never match a Go DOT IMPORT
// (`import . "charm.land/huh/v2"`) -- `\w+` requires at least one word
// character, and a dot import's "alias" token is a literal ".". Worse, a dot
// import makes every one of huh's exported names (NewForm, etc.) callable
// completely bare, with NO package-qualifier prefix at all -- so even a
// denylist that somehow learned to recognize the dot-import syntax itself
// would have nothing left to pattern-match against for the actual calls (no
// "huh." or alias "." prefix appears anywhere in the call site). The first
// AST-based replacement (external audit round 7, 2026-07-31) fixed that, but
// only ever scanned marketplaceInitCmd's OWN function body.
//
// B-BLOCKING-1 (external audit round 8, 2026-07-31 follow-up): that is
// itself bypassable with no aliasing trick at all -- moving the interactive
// call into a SEPARATE package-level helper function (or a package-level var
// directly aliased to an interactive selector, e.g. `var f = ux.Confirm`)
// that marketplaceInitCmd merely calls by name slips straight past a
// checker that only ever looks at marketplaceInitCmd's own body, since the
// actual interactive call textually lives somewhere else entirely. This is
// fixed by treating this as a (bounded) call-graph reachability problem
// instead of a single-function body scan: starting from
// marketplaceInitCmd, every package-level function (or func-literal-valued
// var) reachable through a plain, statically-resolvable identifier call is
// itself scanned the same way, transitively, across every non-test .go file
// in this package (not just marketplace_authoring.go, since a helper could
// be declared in any file in package main) -- see resolveAC53Callables and
// this test's own worklist loop below.
//
// Residual, honestly disclosed scope limit (not "later" -- a specific,
// bounded gap, matching this whole task's fail-closed convention): this
// resolves calls through a directly-named package-level function/var, an
// interface-typed value's method call (conservatively, by method NAME --
// see ac53FindViolations' methodsByName fallback), and every package init()
// (treated as an additional BFS root, see resolveAC53Callables' initRoots).
// It does NOT attempt full points-to analysis for values threaded through
// struct fields or a function VALUE returned by another function call --
// doing so soundly would require go/types (a much larger surface than
// go/ast+go/parser alone, and this project's own convention throughout this
// task has been to avoid adding surface beyond what a concrete, demonstrated
// bypass requires). A fully dynamic indirection (e.g. building a
// `func() error` value at runtime from unrelated inputs, stored in a struct
// field, and invoking it) is not defended against here, and would need
// go/types-based points-to analysis to close soundly.
//
// B-BLOCKING-1 (external audit round 9, 2026-07-31 follow-up): the prior
// version of this gate was itself bypassable with no aliasing or helper-func
// trick, using only a compilable interface + init():
//
//	type ac53Runner interface{ Run() }
//	type ac53Prompt struct{}
//	func (ac53Prompt) Run() { _, _ = ux.Confirm("continue?", false) }
//	var ac53Dynamic ac53Runner
//	func init() { ac53Dynamic = ac53Prompt{} }
//	// marketplaceInitCmd.RunE calls ac53Dynamic.Run()
//
// Two independent gaps combined to let this through: (1) resolveAC53Callables
// only ever collected non-method (d.Recv == nil) FuncDecls, so
// `ac53Prompt.Run`'s body -- the one actually calling ux.Confirm -- was never
// even in the callables map to recurse into; (2) ac53FindViolations only
// matched a *ast.CallExpr whose Fun was a bare *ast.Ident, never a
// *ast.SelectorExpr (`ac53Dynamic.Run()`), and the STANDALONE *ast.SelectorExpr
// visit (for the huh./ux./ck. denylist) only checked c.binds[id.Name] against
// known import bindings, falling through its default: branch with no further
// check for "id.Name is some other identifier of unknown type entirely".
// Fixed by: registering every method declaration under a synthetic
// "ReceiverType.MethodName" callables key; treating a SelectorExpr whose base
// identifier is NOT a known huh/ux import binding as a possible interface
// dispatch, conservatively enqueuing EVERY method sharing that bare selector
// name (methodsByName) as reachable -- this project cannot resolve id's
// static/dynamic type without go/types, so it over-approximates rather than
// risk a false negative; and treating every package init() as an additional
// BFS root alongside marketplaceInitCmd, since init() runs automatically at
// package load and is never reachable through any call-graph edge
// marketplaceInitCmd's own body could name.
func TestMarketplaceInitCmd_NoInteractiveComponents(t *testing.T) {
	fset := token.NewFileSet()
	files, err := ac53ParsePackageFiles(fset, ".")
	if err != nil {
		t.Fatalf("parse package files in .: %v", err)
	}

	callables, aliases, initRoots, err := resolveAC53Callables(files)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := callables["marketplaceInitCmd"]; !ok {
		t.Fatal("marketplaceInitCmd function declaration not found among this package's non-test .go files")
	}

	roots := append([]string{"marketplaceInitCmd"}, initRoots...)
	violations := ac53FindViolations(callables, aliases, roots)
	if len(violations) > 0 {
		t.Fatalf("marketplaceInitCmd (transitively), or a package init() (run automatically at package load, and therefore an equally authoritative entry point for AC53/D13's non-interactivity requirement), contains interactive component call(s), want none: %s", strings.Join(violations, ", "))
	}
}

// ac53ParsePackageFiles parses every non-test .go file directly inside dir
// (no subdirectories -- this package has none) as Go source, returning the
// parsed *ast.File for each. Test files are excluded: a helper function
// TESTS declare for their own fixture purposes is not part of the shipped
// marketplaceInitCmd's real call graph.
func ac53ParsePackageFiles(fset *token.FileSet, dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.AllErrors)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// resolveAC53Callables walks every parsed file and collects:
//   - callables: every top-level func declaration (both plain functions and
//     methods -- a method is keyed by a synthetic "ReceiverType.MethodName",
//     see ac53ReceiverTypeName, so ac53FindViolations' conservative
//     interface-dispatch fallback can still recurse into it even with no
//     directly-named call-graph edge) and every package-level
//     `var NAME = func(...) {...}` literal, each paired with its OWNING
//     FILE's own import bindings.
//   - aliases: every package-level `var NAME = boundIdent.Selector` (a
//     direct alias to an already-bound identifier's selector, e.g.
//     `var f = ux.Confirm`), and `var NAME = otherPackageLevelIdent` chains
//     resolved to whichever of the above (callable or alias) the chain
//     ultimately reaches.
//   - initRoots: a synthetic callables key for every `func init() {...}`
//     declaration found (there can be more than one per file, and more than
//     one per package -- a bare "init" map key would silently keep only the
//     last one collected). init() runs automatically at package load, never
//     through any call site marketplaceInitCmd's own call graph could name,
//     so the caller treats each of these as an ADDITIONAL BFS root alongside
//     marketplaceInitCmd (B-BLOCKING-1, external audit round 9, 2026-07-31).
func resolveAC53Callables(files []*ast.File) (callables map[string]ac53Callable, aliases map[string]ac53Alias, initRoots []string, err error) {
	callables = make(map[string]ac53Callable)
	aliases = make(map[string]ac53Alias)
	pendingIdentAlias := make(map[string]string)

	for _, file := range files {
		binds := make(map[string]string)
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			prefix := matchingInteractivePrefix(path)
			if prefix == "" {
				continue
			}
			// A dot import makes every one of the interactive-capable
			// package's exports callable with NO package-qualifier prefix
			// at all in this file -- unrecoverable for any denylist, AST-
			// based or not (see this test's own doc comment).
			if imp.Name != nil && imp.Name.Name == "." {
				return nil, nil, nil, fmt.Errorf("%s dot-imports %q -- every exported name becomes callable with no package-qualifier prefix at all, which no denylist (regex or AST) can ever positively rule out; AC53 requires marketplace init to stay non-interactive (D13)", file.Name.Name, path)
			}
			if imp.Name != nil && imp.Name.Name == "_" {
				continue // blank import: never referenced, cannot be called
			}
			ident := interactiveDefaultIdent[prefix]
			if imp.Name != nil {
				ident = imp.Name.Name
			}
			binds[ident] = prefix
		}

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				switch {
				case d.Recv != nil:
					// Method declaration (B-BLOCKING-1, round 9): the
					// prior version only ever collected d.Recv == nil
					// (plain function) declarations, so a method like
					// `func (ac53Prompt) Run() { ux.Confirm(...) }` was
					// never in callables at all -- nothing could ever
					// recurse into its body, no matter how it was reached.
					if typeName := ac53ReceiverTypeName(d.Recv); typeName != "" {
						callables[typeName+"."+d.Name.Name] = ac53Callable{body: d.Body, binds: binds}
					}
				case d.Name.Name == "init":
					key := fmt.Sprintf("init#%d", len(initRoots))
					callables[key] = ac53Callable{body: d.Body, binds: binds}
					initRoots = append(initRoots, key)
				default:
					callables[d.Name.Name] = ac53Callable{body: d.Body, binds: binds}
				}
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i >= len(vs.Values) {
							continue
						}
						switch v := vs.Values[i].(type) {
						case *ast.FuncLit:
							callables[name.Name] = ac53Callable{body: v.Body, binds: binds}
						case *ast.SelectorExpr:
							if id, ok := v.X.(*ast.Ident); ok {
								aliases[name.Name] = ac53Alias{prefix: binds[id.Name], selector: v.Sel.Name}
							}
						case *ast.Ident:
							pendingIdentAlias[name.Name] = v.Name
						}
					}
				}
			}
		}
	}

	// Resolve `var a = b` chains (b itself a package-level func/var) to
	// whichever of callables/aliases b ultimately names, up to len+1 passes
	// -- enough to settle any chain no longer than the number of pending
	// aliases itself, without looping forever on a genuine cycle.
	for pass := 0; pass <= len(pendingIdentAlias); pass++ {
		progressed := false
		for name, target := range pendingIdentAlias {
			if c, ok := callables[target]; ok {
				callables[name] = c
				delete(pendingIdentAlias, name)
				progressed = true
			} else if a, ok := aliases[target]; ok {
				aliases[name] = a
				delete(pendingIdentAlias, name)
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}

	return callables, aliases, initRoots, nil
}

// ac53ReceiverTypeName extracts a method declaration's receiver type name,
// stripping a pointer receiver's leading "*" (`func (r *Foo) M()` and
// `func (r Foo) M()` both yield "Foo"). Returns "" for a receiver whose type
// expression isn't a plain (possibly pointer) named identifier -- callers
// skip registering the method in that unlikely case rather than guess.
func ac53ReceiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	expr := recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// ac53FindViolations performs a breadth-first walk of the call graph rooted
// at every name in roots (built from callables/aliases -- see
// resolveAC53Callables; the caller seeds this with "marketplaceInitCmd" plus
// every collected init() synthetic key), returning a human-readable
// description of every interactive-component reference found, transitively.
func ac53FindViolations(callables map[string]ac53Callable, aliases map[string]ac53Alias, roots []string) []string {
	visited := map[string]bool{}
	queue := append([]string{}, roots...)
	var violations []string

	// methodsByName maps a bare method name (e.g. "Run") to every
	// "ReceiverType.MethodName" callables key sharing it (B-BLOCKING-1,
	// external audit round 9, 2026-07-31 follow-up -- see the default:
	// branch below). Plain function/var callables keys never contain "."
	// (Go identifiers cannot), so this only ever picks up the synthetic
	// method keys resolveAC53Callables constructs.
	methodsByName := make(map[string][]string)
	for key := range callables {
		if idx := strings.LastIndex(key, "."); idx >= 0 {
			methodName := key[idx+1:]
			methodsByName[methodName] = append(methodsByName[methodName], key)
		}
	}

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if visited[name] {
			continue
		}
		visited[name] = true

		c, ok := callables[name]
		if !ok {
			continue
		}

		ast.Inspect(c.body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				if !ok {
					return true
				}
				switch c.binds[id.Name] {
				case "charm.land/huh":
					violations = append(violations, id.Name+"."+x.Sel.Name+" (in "+name+")")
				case "github.com/apm-go/apm/internal/ux":
					if interactiveUXSelectors[x.Sel.Name] {
						violations = append(violations, id.Name+"."+x.Sel.Name+" (in "+name+")")
					}
				default:
					if interactiveClackVarNames[id.Name] && interactiveClackVarSelectors[x.Sel.Name] {
						violations = append(violations, id.Name+"."+x.Sel.Name+" (in "+name+")")
					}
					// B-BLOCKING-1 (round 9): id.Name is bound to NEITHER
					// huh nor ux here -- it could be a local variable,
					// parameter, or package-level var of unknown (possibly
					// interface) static type, e.g. `ac53Dynamic.Run()` where
					// ac53Dynamic's declared type is an interface a package
					// init() wires to a concrete implementation at
					// package-load time. This AST-only analysis cannot
					// resolve id's static/dynamic type without go/types, so
					// it conservatively over-approximates: every method
					// declared ANYWHERE in this package under the same bare
					// selector name is treated as reachable, since we
					// cannot positively rule out that id's type is one of
					// their receivers.
					for _, key := range methodsByName[x.Sel.Name] {
						if !visited[key] {
							queue = append(queue, key)
						}
					}
				}
			case *ast.CallExpr:
				id, ok := x.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				if a, ok := aliases[id.Name]; ok {
					if a.prefix == "charm.land/huh" || (a.prefix == "github.com/apm-go/apm/internal/ux" && interactiveUXSelectors[a.selector]) {
						violations = append(violations, id.Name+"() [alias of "+a.prefix+"."+a.selector+"] (in "+name+")")
					}
				}
				if _, ok := callables[id.Name]; ok && !visited[id.Name] {
					queue = append(queue, id.Name)
				}
			}
			return true
		})
	}

	return violations
}

// matchingInteractivePrefix returns the interactiveImportPrefixes entry path
// matches (as an exact match or a "/"-bounded sub-path, e.g.
// "charm.land/huh/v2" matches the "charm.land/huh" prefix), or "" if none
// does.
func matchingInteractivePrefix(path string) string {
	for _, prefix := range interactiveImportPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return prefix
		}
	}
	return ""
}
