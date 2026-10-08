// gatetool is the Go-aware half of tools/gate.sh: it derives the changed-unit
// list and the changed-line coverage accounting from the diff and the AST,
// where a shell script would have to guess from source text.
//
//	gatetool units    -base REF -module PATH -profile cover.out ROOT...
//	gatetool coverage -base REF -module PATH -profile cover.out ROOT...
//	gatetool replace  -file F -old S -new T
//
// Both diff commands take the changed files from "git diff --name-status
// <base>...HEAD" over the whole tree, and the added-line ranges (new side)
// and removed-line ranges (old side) from "git diff -U0". ROOT... are the
// measured roots: they classify the changed subject .go files and never
// narrow the diff. A file under a root is measured; one that is deleted or
// has no changed line (pure rename, mode change) is listed as having no line
// to measure; a file under no root is listed as unmeasured. Every changed
// subject file is therefore in the output. An empty diff exits 2 (wrong
// base, or run on the base itself); a diff with no measured file exits 0 and
// says so. The
// coverage classifier is deliberately conservative: a line is
// non-executable only when the AST shows no statement on it (blank,
// comment, package/import clause, declaration-only line, bare delimiter).
// Any other line with no coverage block is reported as "unmapped" and never
// folded into non-executable. Files the host GOOS does not compile
// (go list IgnoredGoFiles) are reported separately as platform-excluded,
// because their lines can never carry a mapping here.
//
// "replace" is the mutation runner's editor: it substitutes exactly one
// occurrence of -old with -new and exits 2 on zero or many, so a stale
// mutant spec fails the run instead of silently testing nothing.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fatal(2, "usage: gatetool units|coverage|replace ...")
	}
	var err error
	switch os.Args[1] {
	case "units":
		err = runUnits(os.Args[2:])
	case "coverage":
		err = runCoverage(os.Args[2:])
	case "replace":
		err = runReplace(os.Args[2:])
	default:
		fatal(2, "unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		if ge, ok := err.(gateErr); ok {
			fatal(ge.code, "%s", ge.msg)
		}
		fatal(2, "%v", err)
	}
}

type gateErr struct {
	code int
	msg  string
}

func (e gateErr) Error() string { return e.msg }

func fatal(code int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "gatetool: "+format+"\n", a...)
	os.Exit(code)
}

// ---- diff -----------------------------------------------------------------

type lineRange struct{ start, end int } // inclusive

type fileDiff struct {
	path    string
	deleted bool
	added   []lineRange // new side
	removed []lineRange // old side
}

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// gitDiff returns the line ranges of each file that has a text hunk. It is
// not the list of changed files: a pure rename, a mode change and a binary
// file have no hunk. changedFiles is that list.
func gitDiff(base string) (map[string]*fileDiff, error) {
	out, err := exec.Command("git", "diff", "-U0", "--no-color", "--no-ext-diff", base+"...HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	files := map[string]*fileDiff{}
	var cur *fileDiff
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			cur = &fileDiff{}
		case strings.HasPrefix(line, "--- a/"):
			cur.path = strings.TrimPrefix(line, "--- a/")
		case strings.HasPrefix(line, "+++ "):
			if line == "+++ /dev/null" {
				cur.deleted = true
			} else {
				cur.path = strings.TrimPrefix(line, "+++ b/")
			}
			files[cur.path] = cur
		case strings.HasPrefix(line, "@@"):
			m := hunkRe.FindStringSubmatch(line)
			if m == nil || cur == nil {
				return nil, fmt.Errorf("unparseable hunk header: %s", line)
			}
			os, oc := atoi(m[1]), 1
			if m[2] != "" {
				oc = atoi(m[2])
			}
			ns, nc := atoi(m[3]), 1
			if m[4] != "" {
				nc = atoi(m[4])
			}
			if oc > 0 {
				cur.removed = append(cur.removed, lineRange{os, os + oc - 1})
			}
			if nc > 0 {
				cur.added = append(cur.added, lineRange{ns, ns + nc - 1})
			}
		}
	}
	return files, sc.Err()
}

type changedFile struct {
	status  byte   // first letter of the git status: A, M, D, R, C, T
	path    string // new path for a rename or copy
	oldPath string // set for a rename or copy
}

func changedFiles(base string) ([]changedFile, error) {
	out, err := exec.Command("git", "diff", "--name-status", "-z", base+"...HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-status: %w", err)
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	var files []changedFile
	for i := 0; i < len(fields) && fields[i] != ""; i++ {
		cf := changedFile{status: fields[i][0]}
		paths := 1
		if cf.status == 'R' || cf.status == 'C' {
			paths = 2
		}
		if i+paths >= len(fields) {
			return nil, fmt.Errorf("git diff --name-status: truncated record %q", fields[i])
		}
		if paths == 2 {
			cf.oldPath = fields[i+1]
		}
		cf.path = fields[i+paths]
		i += paths
		files = append(files, cf)
	}
	return files, nil
}

// change is the diff split by the measured roots. Each changed subject .go
// file is in exactly one of the three sorted lists.
type change struct {
	diffs map[string]*fileDiff
	roots []string
	// measured: under a root, deleted or with changed lines.
	measured []string
	// lineless: under a root, not deleted, no changed line. Each entry is
	// the path followed by the reason in parentheses.
	lineless []string
	// unmeasured: under no root.
	unmeasured []string
}

func loadChange(sub, base string, roots []string) (*change, error) {
	changed, err := changedFiles(base)
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return nil, gateErr{2, sub + ": the diff is empty (fail closed)"}
	}
	diffs, err := gitDiff(base)
	if err != nil {
		return nil, err
	}
	c := &change{diffs: diffs}
	for _, r := range roots {
		c.roots = append(c.roots, path.Clean(filepath.ToSlash(r)))
	}
	for _, cf := range changed {
		p := cf.path
		if !isSubjectGoFile(p) {
			continue
		}
		d := diffs[p]
		if d == nil {
			d = &fileDiff{path: p}
			diffs[p] = d
		}
		d.deleted = cf.status == 'D'
		switch {
		case !underRoot(p, c.roots):
			c.unmeasured = append(c.unmeasured, p)
		case d.deleted || len(d.added)+len(d.removed) > 0:
			c.measured = append(c.measured, p)
		case cf.oldPath != "":
			c.lineless = append(c.lineless, fmt.Sprintf("%s (renamed from %s, no changed line)", p, cf.oldPath))
		default:
			c.lineless = append(c.lineless, p+" (no changed line)")
		}
	}
	sort.Strings(c.measured)
	sort.Strings(c.lineless)
	sort.Strings(c.unmeasured)
	return c, nil
}

// underRoot matches on a directory boundary: root "cmd" holds "cmd/a/x.go"
// and not "cmdx/y.go".
func underRoot(p string, roots []string) bool {
	for _, r := range roots {
		if p == r || strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return n
}

func isSubjectGoFile(p string) bool {
	return strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") && !strings.Contains(p, "/testdata/")
}

// ---- symbols ----------------------------------------------------------------

type symbol struct {
	name       string
	start, end int
}

func fileSymbols(fset *token.FileSet, f *ast.File) []symbol {
	var syms []symbol
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = recvName(d.Recv.List[0].Type) + "." + name
			}
			syms = append(syms, symbol{name, fset.Position(d.Pos()).Line, fset.Position(d.End()).Line})
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					syms = append(syms, symbol{"type " + s.Name.Name, fset.Position(s.Pos()).Line, fset.Position(s.End()).Line})
				case *ast.ValueSpec:
					var names []string
					for _, n := range s.Names {
						names = append(names, n.Name)
					}
					kw := "var"
					if d.Tok == token.CONST {
						kw = "const"
					}
					syms = append(syms, symbol{kw + " " + strings.Join(names, ","), fset.Position(s.Pos()).Line, fset.Position(s.End()).Line})
				}
			}
		}
	}
	return syms
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	}
	return "?"
}

func enclosing(syms []symbol, line int) string {
	for _, s := range syms {
		if line >= s.start && line <= s.end {
			return s.name
		}
	}
	return "(file-level)"
}

func parseSource(path string, src []byte) (*token.FileSet, *ast.File, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	return fset, f, err
}

// ---- coverage profile -------------------------------------------------------

type block struct {
	start, end int
	count      int
}

func readProfile(path, module string) (map[string][]block, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	blocks := map[string][]block{}
	sc := bufio.NewScanner(fh)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if strings.HasPrefix(line, "mode:") {
				continue
			}
		}
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			return nil, fmt.Errorf("bad profile line: %s", line)
		}
		file := strings.TrimPrefix(line[:colon], module+"/")
		var sl, scol, el, ec, n, cnt int
		if _, err := fmt.Sscanf(line[colon+1:], "%d.%d,%d.%d %d %d", &sl, &scol, &el, &ec, &n, &cnt); err != nil {
			return nil, fmt.Errorf("bad profile line: %s", line)
		}
		blocks[file] = append(blocks[file], block{sl, el, cnt})
	}
	return blocks, sc.Err()
}

// ---- executable-line classifier ---------------------------------------------

// execLines marks the lines that carry an executable statement. Compound
// statements contribute their header line only (their bodies are visited
// separately); simple statements contribute every line they span.
func execLines(fset *token.FileSet, f *ast.File, src []byte) map[int]bool {
	lines := map[int]bool{}
	srcLines := strings.Split(string(src), "\n")
	mark := func(from, to token.Pos) {
		a, b := fset.Position(from).Line, fset.Position(to).Line
		for l := a; l <= b; l++ {
			lines[l] = true
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(ast.Stmt)
		if !ok {
			return true
		}
		defer func() {
			// Within a multi-line statement, a line holding only closing
			// delimiters or a comment executes nothing; drop it after the
			// statement's range was marked so the exclusion stays narrow.
			for l := fset.Position(st.Pos()).Line; l <= fset.Position(st.End()).Line; l++ {
				if lines[l] && isDelimiterOrCommentLine(srcLines, l) {
					delete(lines, l)
				}
			}
		}()
		switch s := st.(type) {
		case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
			// Blocks and case/default labels carry no code of their own;
			// go cover never emits a block for a label line, so marking
			// it would report a classifier artefact as "unmapped".
		case *ast.LabeledStmt,
			*ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			mark(st.Pos(), st.Pos())
		case *ast.IfStmt:
			mark(s.Pos(), s.Pos())
			if s.Else != nil {
				mark(s.Else.Pos(), s.Else.Pos())
			}
		default:
			mark(st.Pos(), st.End())
		}
		return true
	})
	return lines
}

// isDelimiterOrCommentLine reports whether 1-based line l of src is only
// closing delimiters (e.g. "}", "})", "}()", "},") or a line comment.
func isDelimiterOrCommentLine(srcLines []string, l int) bool {
	if l < 1 || l > len(srcLines) {
		return false
	}
	t := strings.TrimSpace(srcLines[l-1])
	if t == "" || strings.HasPrefix(t, "//") {
		return true
	}
	return strings.Trim(t, "}]),(") == ""
}

func ignoredGoFiles(dirs []string) (map[string]bool, error) {
	set := map[string]bool{}
	if len(dirs) == 0 {
		return set, nil
	}
	args := append([]string{"list", "-e", "-json=Dir,IgnoredGoFiles"}, dirs...)
	out, err := exec.Command("go", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p struct {
			Dir            string
			IgnoredGoFiles []string
		}
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		for _, f := range p.IgnoredGoFiles {
			set[filepath.ToSlash(filepath.Join(p.Dir, f))] = true
		}
	}
	return set, nil
}

// ---- units ----------------------------------------------------------------------

var testFuncRe = regexp.MustCompile(`(?m)^func (Test\w+|Example\w*|Fuzz\w+)\(`)

// directTests lists test functions in the package directory whose body
// mentions the bare symbol name. This is a textual reference, recorded as
// such; coverage is the executable evidence.
func directTests(dir, sym string) []string {
	bare := sym
	if i := strings.LastIndex(bare, "."); i >= 0 {
		bare = bare[i+1:]
	}
	if i := strings.Index(bare, " "); i >= 0 { // "type X", "var a,b"
		bare = bare[i+1:]
		if j := strings.Index(bare, ","); j >= 0 {
			bare = bare[:j]
		}
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	wordRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(bare) + `\b`)
	var hits []string
	for _, tf := range entries {
		src, err := os.ReadFile(tf)
		if err != nil {
			continue
		}
		locs := testFuncRe.FindAllSubmatchIndex(src, -1)
		for i, loc := range locs {
			end := len(src)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			if wordRe.Match(src[loc[0]:end]) {
				hits = append(hits, filepath.Base(tf)+"::"+string(src[loc[2]:loc[3]]))
			}
		}
	}
	sort.Strings(hits)
	if len(hits) > 6 {
		hits = append(hits[:6], fmt.Sprintf("(+%d more)", len(hits)-6))
	}
	return hits
}

func runUnits(args []string) error {
	fs := flag.NewFlagSet("units", flag.ExitOnError)
	base := fs.String("base", "main", "base ref")
	module := fs.String("module", "", "module path (for profile file names)")
	profile := fs.String("profile", "", "coverage profile")
	fs.Parse(args)
	if fs.NArg() == 0 || *module == "" || *profile == "" {
		return gateErr{2, "units: -module, -profile and at least one measured root are required"}
	}
	ch, err := loadChange("units", *base, fs.Args())
	if err != nil {
		return err
	}
	blocks, err := readProfile(*profile, *module)
	if err != nil {
		return err
	}
	fmt.Println("unit\tfile\tlines(covered/exec)\tdirect-tests")
	total := 0
	for _, p := range ch.measured {
		d := ch.diffs[p]
		if d.deleted {
			// Removed file: every symbol it held is a deleted unit; the
			// build + suite passing is what shows nothing depended on it.
			oldSrc, err := exec.Command("git", "show", *base+":"+p).Output()
			if err != nil {
				return fmt.Errorf("git show %s: %w", p, err)
			}
			fset, f, err := parseSource(p, oldSrc)
			if err != nil {
				return err
			}
			for _, s := range fileSymbols(fset, f) {
				fmt.Printf("deleted: %s\t%s\t-\tbuild+suite green (no remaining reference)\n", s.name, p)
				total++
			}
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fset, f, err := parseSource(p, src)
		if err != nil {
			return err
		}
		syms := fileSymbols(fset, f)
		execL := execLines(fset, f, src)
		units := map[string][2]int{} // covered, exec
		order := []string{}
		for _, r := range d.added {
			for l := r.start; l <= r.end; l++ {
				name := enclosing(syms, l)
				if _, seen := units[name]; !seen {
					order = append(order, name)
					units[name] = [2]int{}
				}
				if !execL[l] {
					continue
				}
				c := units[name]
				c[1]++
				if coveredAt(blocks[p], l) {
					c[0]++
				}
				units[name] = c
			}
		}
		// Symbols present at base but absent now = deleted units.
		if len(d.removed) > 0 {
			if oldSrc, err := exec.Command("git", "show", *base+":"+p).Output(); err == nil {
				if ofset, of, perr := parseSource(p, oldSrc); perr == nil {
					now := map[string]bool{}
					for _, s := range syms {
						now[s.name] = true
					}
					oldSyms := fileSymbols(ofset, of)
					for _, r := range d.removed {
						for l := r.start; l <= r.end; l++ {
							name := enclosing(oldSyms, l)
							if name != "(file-level)" && !now[name] {
								key := "deleted: " + name
								if _, seen := units[key]; !seen {
									order = append(order, key)
									units[key] = [2]int{-1, -1}
								}
							}
						}
					}
				}
			}
		}
		for _, name := range order {
			c := units[name]
			total++
			if c[1] == -1 {
				fmt.Printf("%s\t%s\t-\tbuild+suite green (no remaining reference)\n", name, p)
				continue
			}
			tests := directTests(filepath.Dir(p), name)
			if len(tests) == 0 {
				tests = []string{"(no direct textual reference; coverage only)"}
			}
			fmt.Printf("%s\t%s\t%d/%d\t%s\n", name, p, c[0], c[1], strings.Join(tests, ", "))
		}
	}
	for _, e := range ch.lineless {
		fmt.Printf("# no line to measure: %s\n", e)
	}
	for _, p := range ch.unmeasured {
		fmt.Printf("# unmeasured: %s\n", p)
	}
	fmt.Printf("# units: %d, unmeasured files: %d, measured roots: %s, granularity: symbol (enclosing top-level decl of each changed line)\n",
		total, len(ch.unmeasured), strings.Join(ch.roots, " "))
	return nil
}

func coveredAt(bl []block, line int) bool {
	for _, b := range bl {
		if line >= b.start && line <= b.end && b.count > 0 {
			return true
		}
	}
	return false
}

func mappedAt(bl []block, line int) bool {
	for _, b := range bl {
		if line >= b.start && line <= b.end {
			return true
		}
	}
	return false
}

// ---- coverage --------------------------------------------------------------------

func runCoverage(args []string) error {
	fs := flag.NewFlagSet("coverage", flag.ExitOnError)
	base := fs.String("base", "main", "base ref")
	module := fs.String("module", "", "module path")
	profile := fs.String("profile", "", "coverage profile")
	fs.Parse(args)
	if fs.NArg() == 0 || *module == "" || *profile == "" {
		return gateErr{2, "coverage: -module, -profile and at least one measured root are required"}
	}
	ch, err := loadChange("coverage", *base, fs.Args())
	if err != nil {
		return err
	}
	diffs := ch.diffs
	blocks, err := readProfile(*profile, *module)
	if err != nil {
		return err
	}
	var files, dirs []string
	dirSeen := map[string]bool{}
	noLines := append([]string{}, ch.lineless...)
	for _, p := range ch.measured {
		if diffs[p].deleted {
			noLines = append(noLines, p+" (deleted)")
		} else {
			files = append(files, p)
			dir := "./" + filepath.ToSlash(filepath.Dir(p))
			if !dirSeen[dir] {
				dirSeen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	ignored, err := ignoredGoFiles(dirs)
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	var covered, execMapped, unmapped, nonexec, platform int
	var missList, unmappedList, platformFiles []string
	for _, p := range files {
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fset, f, err := parseSource(p, src)
		if err != nil {
			return err
		}
		execL := execLines(fset, f, src)
		abs := filepath.ToSlash(filepath.Join(cwd, p))
		isPlatformExcluded := ignored[abs]
		fileLines := 0
		for _, r := range diffs[p].added {
			for l := r.start; l <= r.end; l++ {
				if !execL[l] {
					nonexec++
					continue
				}
				if isPlatformExcluded {
					platform++
					fileLines++
					continue
				}
				if !mappedAt(blocks[p], l) {
					unmapped++
					unmappedList = append(unmappedList, fmt.Sprintf("%s:%d", p, l))
					continue
				}
				execMapped++
				if coveredAt(blocks[p], l) {
					covered++
				} else {
					missList = append(missList, fmt.Sprintf("%s:%d", p, l))
				}
			}
		}
		if isPlatformExcluded && fileLines > 0 {
			platformFiles = append(platformFiles, fmt.Sprintf("%s (%d lines)", p, fileLines))
		}
	}
	pct := 0.0
	if execMapped > 0 {
		pct = 100 * float64(covered) / float64(execMapped)
	}
	fmt.Printf("changed-line coverage: covered=%d exec_mapped=%d (%.1f%%) unmapped=%d platform_excluded=%d nonexec=%d files=%d unmeasured=%d no_lines=%d\n",
		covered, execMapped, pct, unmapped, platform, nonexec, len(files), len(ch.unmeasured), len(noLines))
	fmt.Println("classifier: set2 (non-executable) = lines with no ast.Stmt on them (blank, comment, package/import, declaration-only, bare delimiter); set1 = executable lines inside a profile block; set3 (unmapped) = executable lines with no profile block, never folded into set2")
	if len(ch.measured)+len(ch.lineless) == 0 {
		fmt.Printf("no measured file in the diff (measured roots: %s)\n", strings.Join(ch.roots, " "))
	}
	if len(noLines) > 0 {
		sort.Strings(noLines)
		fmt.Printf("files with no line to measure (%d), inside the measured roots:\n", len(noLines))
		for _, e := range noLines {
			fmt.Println("  " + e)
		}
	}
	if len(ch.unmeasured) > 0 {
		fmt.Printf("UNMEASURED files (%d), outside the measured roots:\n", len(ch.unmeasured))
		for _, p := range ch.unmeasured {
			fmt.Println("  " + p)
		}
	}
	if len(platformFiles) > 0 {
		fmt.Println("platform-excluded files (not compiled on this GOOS, no mapping possible here):")
		for _, s := range platformFiles {
			fmt.Println("  " + s)
		}
	}
	if len(unmappedList) > 0 {
		fmt.Printf("UNMAPPED executable lines (%d):\n", len(unmappedList))
		for _, s := range compressRanges(unmappedList) {
			fmt.Println("  " + s)
		}
	}
	if len(missList) > 0 {
		fmt.Printf("UNCOVERED executable lines (%d):\n", len(missList))
		for _, s := range compressRanges(missList) {
			fmt.Println("  " + s)
		}
	}
	if unmapped > 0 || covered < execMapped {
		return gateErr{1, fmt.Sprintf("coverage threshold missed: covered %d/%d, unmapped %d", covered, execMapped, unmapped)}
	}
	return nil
}

// compressRanges folds "f:10 f:11 f:12" into "f:10-12" for readable reports.
func compressRanges(items []string) []string {
	var out []string
	var curFile string
	var start, prev int
	flush := func() {
		if curFile == "" {
			return
		}
		if start == prev {
			out = append(out, fmt.Sprintf("%s:%d", curFile, start))
		} else {
			out = append(out, fmt.Sprintf("%s:%d-%d", curFile, start, prev))
		}
	}
	for _, it := range items {
		i := strings.LastIndex(it, ":")
		f, n := it[:i], atoi(it[i+1:])
		if f == curFile && n == prev+1 {
			prev = n
			continue
		}
		flush()
		curFile, start, prev = f, n, n
	}
	flush()
	return out
}

// ---- replace -------------------------------------------------------------------

func runReplace(args []string) error {
	fs := flag.NewFlagSet("replace", flag.ExitOnError)
	file := fs.String("file", "", "file to edit")
	old := fs.String("old", "", "exact text to replace (must occur once)")
	nw := fs.String("new", "", "replacement text")
	fs.Parse(args)
	if *file == "" || *old == "" {
		return gateErr{2, "replace: -file and -old are required"}
	}
	src, err := os.ReadFile(*file)
	if err != nil {
		return gateErr{2, err.Error()}
	}
	n := bytes.Count(src, []byte(*old))
	if n != 1 {
		return gateErr{2, fmt.Sprintf("replace: %q occurs %d times in %s, want exactly 1", *old, n, *file)}
	}
	if err := os.WriteFile(*file, bytes.Replace(src, []byte(*old), []byte(*nw), 1), 0o644); err != nil {
		return gateErr{2, err.Error()}
	}
	return nil
}
