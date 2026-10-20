package deploy

import (
	"errors"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/archive"
)

// The link rewriting of a deploy, a port of the Oracle's UnifiedLinkResolver
// as install uses it (compilation/link_resolver.py:106-148,217-544,
// integration/base_integrator.py:839-921).

// windowsPaths is whether paths are read as Python reads them on Windows:
// "\" separates, and names match without regard to case.
var windowsPaths = runtime.GOOS == "windows"

// linkPattern is UnifiedLinkResolver.LINK_PATTERN, and linkTargetPattern the
// one resolve_links counts the rewritten links with.
var (
	linkPattern       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	linkTargetPattern = regexp.MustCompile(`\]\(([^)]+)\)`)
)

// linkRun is what the link rewriting of one Run needs: the deploy root the
// Oracle calls project_root, in the frame of absolute paths it computes in,
// and the context registries it built for each package.
type linkRun struct {
	// logical is the real deploy root as the process names it, and root the
	// same directory as the Oracle names it: Path.cwd(), which has no
	// symlink, for a project, and Path.home() for --global.
	logical, root string
	// deployDir is where this Run writes, a temporary directory for a trial.
	deployDir  string
	registries map[string]map[string]string
}

func newLinkRun(realRoot, deployDir string, global bool) *linkRun {
	logical, err := filepath.Abs(realRoot)
	if err != nil {
		return nil
	}
	root := logical
	if !global {
		if r, err := filepath.EvalSymlinks(logical); err == nil {
			root = r
		}
	}
	return &linkRun{logical: logical, root: root, deployDir: deployDir, registries: map[string]map[string]string{}}
}

// frame is p as an absolute path in the Oracle's frame.
func (r *linkRun) frame(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if rel, ok := pathWithin(r.logical, abs); ok {
		return filepath.Join(r.root, rel)
	}
	return abs
}

// linkSource is the LinkResolutionContext of one file.
type linkSource struct {
	run *linkRun
	// file is the source as the Oracle reads it from: a dependency's with no
	// symlink resolved, since its apm_modules holds none.
	file string
	// pkg is the package root (LinkResolutionContext.package_root), "" when
	// the Oracle rewrites context links alone (--global's own content).
	pkg string
	// dep tells that the package is a dependency, whose paths are taken as
	// they are written; the project's own are resolved, as the Oracle does.
	dep bool
	// preserved is the skill directory a skill's file was copied with.
	preserved string
	target    string
	registry  map[string]string
}

// linkSourceOf is the context of p's file origin, deployed to destRel.
func linkSourceOf(p Primitive, origin, destRel string) (*linkSource, bool) {
	r := activeDeployMode.links
	if r == nil {
		return nil, false
	}
	s := &linkSource{run: r, file: r.frame(origin), dep: p.DepKey != "", target: r.frame(filepath.Join(r.logical, filepath.FromSlash(path.Dir(destRel))))}
	pkgDir := activeDeployMode.projectDir
	if s.dep {
		pkgDir = p.PackageDir
	}
	var scan []string
	switch {
	case s.dep:
		s.pkg = r.frame(pkgDir)
		scan = []string{s.pkg}
	case activeDeployMode.global:
		// The Oracle's install_path of --global's own content is HOME: it
		// scans ~/.apm/ and sets no package root (base_integrator.py:858-889).
		scan = []string{r.frame(pkgDir)}
	default:
		s.pkg = r.frame(pkgDir)
		scan = []string{filepath.Join(s.pkg, ".apm"), filepath.Join(s.pkg, ".github")}
	}
	key := strings.Join(scan, "\x00")
	reg, ok := r.registries[key]
	if !ok {
		reg = contextRegistry(scan)
		r.registries[key] = reg
	}
	s.registry = reg
	return s, true
}

// rewriteLinks is p's content written to destRel with its links rewritten
// (resolve_links_for_installation); content that is not UTF-8 is kept.
func rewriteLinks(p Primitive, destRel string, content []byte) []byte {
	if !utf8.Valid(content) {
		return content
	}
	s, ok := linkSourceOf(p, p.origin(), destRel)
	if !ok {
		return content
	}
	return []byte(s.rewrite(string(content)))
}

// rewriteSkillFile is the file rel of the skill p, data, as the Oracle writes
// it below destDir, and false when the copy is kept: a file not named *.md,
// one that is not UTF-8 (issue #126 comment 6267289737 item 3(2)), and one
// with no link rewritten (skill_integrator.py:553-575).
func rewriteSkillFile(p Primitive, destDir, rel string, data []byte) ([]byte, bool) {
	if !skillMarkdown(path.Base(rel)) || !utf8.Valid(data) {
		return nil, false
	}
	origin := p.origin()
	s, ok := linkSourceOf(p, filepath.Join(origin, filepath.FromSlash(rel)), path.Join(destDir, rel))
	if !ok {
		return nil, false
	}
	s.preserved = s.run.frame(origin)
	content := string(oracleText(data))
	resolved := s.rewrite(content)
	if resolved == content || rewrittenCount(content, resolved) == 0 {
		return nil, false
	}
	return []byte(resolved), true
}

// skillMarkdown is whether a skill file of this name matches the Oracle's
// rglob("*.md"), without regard to case on Windows.
func skillMarkdown(name string) bool {
	if windowsPaths {
		name = strings.ToLower(name)
	}
	return strings.HasSuffix(name, ".md")
}

// rewrittenCount is resolve_links' count: the links of the original that the
// rewritten content no longer has.
func rewrittenCount(original, resolved string) int {
	have := map[string]bool{}
	for _, m := range linkTargetPattern.FindAllStringSubmatch(resolved, -1) {
		have[m[1]] = true
	}
	gone := map[string]bool{}
	for _, m := range linkTargetPattern.FindAllStringSubmatch(original, -1) {
		if !have[m[1]] {
			gone[m[1]] = true
		}
	}
	return len(gone)
}

// rewrite is _rewrite_markdown_links.
func (s *linkSource) rewrite(content string) string {
	return linkPattern.ReplaceAllStringFunc(content, func(match string) string {
		m := linkPattern.FindStringSubmatch(match)
		text, link := m[1], m[2]
		if isExternalURL(link) {
			return match
		}
		var out string
		var ok bool
		switch {
		case isContextLink(link):
			out, ok = s.contextLink(link)
		case s.pkg != "":
			if !rewritableRelativeLink(link) {
				return match
			}
			out, ok = s.assetLink(link)
		}
		if !ok {
			return match
		}
		return "[" + text + "](" + out + ")"
	})
}

// contextLink is _resolve_context_link: the context file by its name in the
// registry, else by the link from the source's directory, else from the
// deploy root, with no bound.
func (s *linkSource) contextLink(link string) (string, bool) {
	actual, ok := s.registry[pyPathName(link)]
	if !ok {
		actual, ok = s.locate(filepath.Dir(s.file), link)
	}
	if !ok {
		actual, ok = resolvedExisting(pyJoin(s.run.root, link))
	}
	if !ok {
		return "", false
	}
	if _, err := os.Stat(actual); err != nil {
		return "", false
	}
	return portableLinkRelpath(actual, s.target)
}

// locate is the path link names from dir, when something is there: for a
// dependency as written, for the project's own content resolved.
func (s *linkSource) locate(dir, link string) (string, bool) {
	p := pyJoin(dir, link)
	if !s.dep {
		return resolvedExisting(p)
	}
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return diskCase(p), true
}

// diskCase is p with each component named as its directory lists it, as
// Path.resolve() names it on Windows (link/win/out-links126.txt); elsewhere
// p as it is.
func diskCase(p string) string {
	if !windowsPaths {
		return p
	}
	vol := filepath.VolumeName(p)
	cur := vol + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(p[len(vol):], string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		name := part
		if entries, err := os.ReadDir(cur); err == nil {
			if i := slices.IndexFunc(entries, func(e os.DirEntry) bool { return strings.EqualFold(e.Name(), part) }); i >= 0 {
				name = entries[i].Name()
			}
		}
		cur = filepath.Join(cur, name)
	}
	return cur
}

// assetLink is _resolve_in_package_asset_link: a regular file of the package
// is named from the target's directory as it is below apm_modules/<key>, or
// below the project, a #fragment or ?query kept.
func (s *linkSource) assetLink(link string) (string, bool) {
	pathPart, suffix := splitLinkTarget(link)
	if pathPart == "" {
		return "", false
	}
	var rel string
	if s.dep {
		candidate := diskCase(pyJoin(filepath.Dir(s.file), pathPart))
		var ok bool
		if rel, ok = pathWithin(s.pkg, candidate); !ok {
			return "", false
		}
		resolved, err := archive.ResolveIn(s.pkg, filepath.ToSlash(rel))
		if err != nil {
			return "", false
		}
		if info, err := os.Stat(filepath.Join(s.pkg, filepath.FromSlash(resolved))); err != nil || !info.Mode().IsRegular() {
			return "", false
		}
		if s.preserved != "" {
			if _, in := pathWithin(s.preserved, candidate); in {
				return "", false
			}
		}
	} else {
		candidate, ok := resolvedExisting(pyJoin(filepath.Dir(s.file), pathPart))
		if !ok {
			return "", false
		}
		if info, err := os.Stat(candidate); err != nil || !info.Mode().IsRegular() {
			return "", false
		}
		if s.preserved != "" {
			if real, err := filepath.EvalSymlinks(s.preserved); err == nil {
				if _, in := pathWithin(real, candidate); in {
					return "", false
				}
			}
		}
		real, err := filepath.EvalSymlinks(s.pkg)
		if err != nil {
			return "", false
		}
		if rel, ok = pathWithin(real, candidate); !ok {
			return "", false
		}
	}
	out, ok := portableLinkRelpath(filepath.Join(s.pkg, rel), s.target)
	if !ok {
		return "", false
	}
	return out + suffix, true
}

// resolvedExisting is Path.resolve() of p when it exists.
func resolvedExisting(p string) (string, bool) {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	if abs, err := filepath.Abs(real); err == nil {
		real = abs
	}
	return real, true
}

// portableLinkRelpath is utils/paths.py:32-43: os.path.relpath with "/", and
// nothing across drives.
func portableLinkRelpath(p, base string) (string, bool) {
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// pathWithin is p below base, or base itself, as is_relative_to tells it.
func pathWithin(base, p string) (string, bool) {
	rel, err := filepath.Rel(base, p)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// pyJoin is pathlib's dir / p: a rooted p replaces dir, keeping dir's drive
// on Windows.
func pyJoin(dir, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if windowsPaths && (strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "/")) {
		return filepath.Join(filepath.VolumeName(dir), p)
	}
	return filepath.Join(dir, p)
}

// pyPathName is Path(link).name.
func pyPathName(link string) string {
	if windowsPaths {
		link = strings.ReplaceAll(link, `\`, "/")
		if len(link) >= 2 && link[1] == ':' {
			link = link[2:]
		}
	}
	parts := strings.Split(link, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" && parts[i] != "." {
			return parts[i]
		}
	}
	return ""
}

// isContextLink is _is_context_file.
func isContextLink(link string) bool {
	l := strings.ToLower(link)
	return strings.HasSuffix(l, ".context.md") || strings.HasSuffix(l, ".memory.md")
}

// isExternalURL is _is_external_url: http or https with a netloc.
func isExternalURL(link string) bool {
	scheme, netloc, err := urlSplit(pyStrip(link))
	return err == nil && (scheme == "http" || scheme == "https") && netloc != ""
}

// rewritableRelativeLink is _is_rewritable_relative_link.
func rewritableRelativeLink(link string) bool {
	stripped := pyStrip(link)
	if stripped == "" || strings.HasPrefix(stripped, "#") || strings.HasPrefix(stripped, "/") {
		return false
	}
	scheme, _, err := urlSplit(stripped)
	return err == nil && scheme == ""
}

// splitLinkTarget is _split_link_target: the path, and from the first # or ?
// on, the rest.
func splitLinkTarget(link string) (string, string) {
	if i := strings.IndexAny(link, "#?"); i >= 0 {
		return link[:i], link[i:]
	}
	return link, ""
}

const schemeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-."

var errInvalidIPv6URL = errors.New("Invalid IPv6 URL")

// urlSplit is the scheme and netloc of Python 3.12's urllib.parse.urlsplit,
// and its ValueError for a bracketed netloc it refuses. The NFKC check of a
// non-ASCII netloc (_checknetloc) is not ported.
func urlSplit(url string) (scheme, netloc string, err error) {
	url = strings.TrimLeftFunc(url, func(r rune) bool { return r <= ' ' })
	url = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(url)
	if i := strings.IndexByte(url, ':'); i > 0 && (url[0] >= 'a' && url[0] <= 'z' || url[0] >= 'A' && url[0] <= 'Z') {
		if strings.Trim(url[:i], schemeChars) == "" {
			scheme, url = strings.ToLower(url[:i]), url[i+1:]
		}
	}
	if strings.HasPrefix(url, "//") {
		netloc = url[2:]
		if j := strings.IndexAny(netloc, "/?#"); j >= 0 {
			netloc = netloc[:j]
		}
		open, close := strings.Contains(netloc, "["), strings.Contains(netloc, "]")
		if open != close {
			return "", "", errInvalidIPv6URL
		}
		if open {
			if err := checkBracketedNetloc(netloc); err != nil {
				return "", "", err
			}
		}
	}
	return scheme, netloc, nil
}

var ipvFutureRE = regexp.MustCompile(`^v[a-fA-F0-9]+\.[^\n]+$`)

// checkBracketedNetloc is _check_bracketed_netloc.
func checkBracketedNetloc(netloc string) error {
	host := netloc[strings.LastIndex(netloc, "@")+1:]
	var hostname string
	if before, bracketed, found := strings.Cut(host, "["); found {
		if before != "" {
			return errInvalidIPv6URL
		}
		var port string
		hostname, port, _ = strings.Cut(bracketed, "]")
		if port != "" && !strings.HasPrefix(port, ":") {
			return errInvalidIPv6URL
		}
	} else {
		hostname, _, _ = strings.Cut(host, ":")
	}
	if strings.HasPrefix(hostname, "v") {
		if !ipvFutureRE.MatchString(hostname) {
			return errors.New("IPvFuture address is invalid")
		}
		return nil
	}
	ip, err := netip.ParseAddr(hostname)
	if err != nil {
		return err
	}
	if ip.Is4() {
		return errors.New("An IPv4 address cannot be in brackets")
	}
	return nil
}

// contextSkipDirs is DEFAULT_SKIP_DIRS (constants.py:41-73): no context file
// below one of them is registered.
var contextSkipDirs = []string{".git", "node_modules", "__pycache__", ".pytest_cache", ".venv", "venv", ".tox", "build", "dist", ".mypy_cache", "apm_modules", "vendor", "third_party", "Pods", "bower_components", "jspm_packages", ".gradle", "target", ".next", ".nuxt", ".cache", ".turbo"}

// contextRegistry is register_contexts over discover_primitives of each
// root that is a directory, in order (primitives/discovery.py:119-180,
// 591-670, compilation/inventory.py:55-90): every regular file named
// *.context.md or *.memory.md below the root, its path resolved, walked
// without following a symlink, a directory's files in name order before its
// subdirectories, leaving out a directory below the root that holds a .git
// and one named in contextSkipDirs. A file whose frontmatter does not load is
// not registered. Of the contexts of one root with one name the first is
// kept, and a later root's file of one file name replaces an earlier one's.
func contextRegistry(roots []string) map[string]string {
	reg := map[string]string{}
	for _, root := range roots {
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		real, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		var files []string
		walkContexts(real, real, &files)
		for _, f := range files {
			name := contextName(filepath.Base(f))
			if seen[name] {
				continue
			}
			seen[name] = true
			reg[filepath.Base(f)] = f
		}
	}
	return reg
}

func walkContexts(root, dir string, files *[]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	if dir != root && slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() == ".git" }) {
		return
	}
	var subdirs []string
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch {
		case e.Type()&os.ModeSymlink != 0:
		case e.IsDir():
			if !slices.Contains(contextSkipDirs, e.Name()) {
				subdirs = append(subdirs, p)
			}
		case e.Type().IsRegular() && contextFile(p):
			*files = append(*files, p)
		}
	}
	for _, d := range subdirs {
		walkContexts(root, d, files)
	}
}

// contextFile is whether the file p is a context the Oracle registers: its
// name matches a context pattern, it parses as a context
// (primitives/parser.py:52-93,258-269), and its frontmatter loads.
func contextFile(p string) bool {
	name := filepath.Base(p)
	matched := strings.ToLower(name)
	if !windowsPaths {
		matched = name
	}
	if !strings.HasSuffix(matched, ".context.md") && !strings.HasSuffix(matched, ".memory.md") {
		return false
	}
	parent := filepath.Dir(p)
	inMemoryDir := filepath.Base(parent) == "memory" && slices.Contains([]string{".apm", ".github"}, filepath.Base(filepath.Dir(parent)))
	if !strings.HasSuffix(name, ".context.md") && !strings.HasSuffix(name, ".memory.md") && !inMemoryDir {
		return false
	}
	content, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	_, err = readPromptDoc(content)
	return err == nil
}

// contextName is _extract_primitive_name of a context file.
func contextName(base string) string {
	for _, ext := range []string{".context.md", ".memory.md", ".md"} {
		if strings.HasSuffix(base, ext) {
			return strings.ReplaceAll(base, ext, "")
		}
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}
