package main

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/mcpregistry"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/yamlcore"
)

// mcpInstallOpts holds every --mcp-related flag/arg from `apm install`.
type mcpInstallOpts struct {
	Name        string
	Transport   string
	URL         string
	EnvPairs    []string
	HeaderPairs []string
	Version     string
	Registry    string
	Force       bool
	Command     []string // stdio command argv, from the `--` separator
	PrePackages []string // positional args before `--` (must be empty with --mcp)
	SkillSubset []string
	TargetFlag  string
}

// runMCPInstall handles `apm install --mcp NAME [flags]`: a standalone
// "declare + deploy this one MCP server" operation, independent of the
// normal apm-package resolve/lockfile pipeline (apm.lock.yaml is untouched).
func runMCPInstall(opts mcpInstallOpts) error {
	if err := validateMCPConflicts(opts); err != nil {
		return err
	}

	data, err := os.ReadFile("apm.yml")
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("apm.yml: no apm.yml found; run 'apm-go init' first")
		}
		return fmt.Errorf("read apm.yml: %w", err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return fmt.Errorf("parse apm.yml: %w", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		return fmt.Errorf("validate apm.yml: %w", err)
	}

	// Compute the apm.yml value FIRST -- this is pure and requires no
	// network call, even for a registry-backed entry (buildPersistEntry
	// never resolves the registry, only opts). Checking identity before any
	// expensive/networked work matches the Python original's actual order:
	// an unchanged entry is a pure local no-op, never touching the
	// registry. Round 2 originally deferred this ordering fix (reasoning
	// unchanged-skips-deploy alone was enough parity); round 3 found that
	// was incomplete -- without this split, apm-go would still make a
	// registry HTTP call (and could fail on outage) for an entry that was
	// never going to change or redeploy anyway.
	entryNode, err := buildPersistEntry(opts)
	if err != nil {
		return err
	}

	// On an interactive TTY a conflicting entry is resolved by prompting
	// (show the diff, default No) instead of hard-failing; non-interactively
	// confirm stays nil so upsertMCPEntry demands --force. Mirrors the Python
	// original's writer.py three-way.
	var confirm confirmReplaceFunc
	if canPromptCreds() {
		confirm = promptReplaceMCP
	}
	status, err := upsertMCPEntry(node, opts.Name, entryNode, opts.Force, confirm)
	if err != nil {
		return err
	}
	if status == "unchanged" || status == "skipped" {
		// unchanged: identical entry. skipped: the user declined the replace
		// prompt -- both leave apm.yml untouched and deploy nothing. A stale
		// or missing deployed target file for an unchanged entry is a
		// pre-existing limitation of --mcp itself (present in the Python
		// original too, per source reading during design): re-run `apm
		// install` (the full pipeline) or delete+re-add the entry to force
		// redeployment.
		ux.Info(os.Stdout, "MCP server %q unchanged", opts.Name)
		return nil
	}

	// Only now -- once we know this call actually changes something --
	// resolve the deployable dep. A registry lookup failure here still
	// leaves apm.yml on disk untouched (AC6): entryNode has only been
	// applied to the in-memory node, not yet serialized/written.
	deployDep, diags, err := buildDeployDep(opts)
	if err != nil {
		return err
	}
	for _, d := range diags {
		ux.Warn(os.Stderr, "%s", d)
	}

	// This edit only ever touches dependencies.mcp: prefer a surgical patch
	// that preserves every other byte of the original apm.yml (including
	// hand-formatted multi-line flow content a full SafeDump re-encode
	// cannot reproduce). Fall back to a full re-encode if the document's
	// shape doesn't fit the patcher's assumptions.
	manifestBytes, patched, err := yamlcore.PatchMappingPath(data, node, []string{"dependencies", "mcp"})
	if err != nil {
		return fmt.Errorf("serialize apm.yml: %w", err)
	}
	if !patched {
		manifestBytes, err = yamlcore.SafeDump(node)
		if err != nil {
			return fmt.Errorf("serialize apm.yml: %w", err)
		}
	}
	if err := os.WriteFile("apm.yml", manifestBytes, 0644); err != nil {
		return fmt.Errorf("write apm.yml: %w", err)
	}

	// Print target source (--target > apm.yml targets: > auto-detect) before
	// deploying, matching runInstall's existing convention (deployAndFinalize)
	// and design.md §8 -- R7 requires the resolved target source be
	// verifiable in stdout, not just the deploy/skip outcome (codex review).
	targets, targetDiags := deploy.ResolveTargets(opts.TargetFlag, m.Target, ".")
	for _, d := range targetDiags {
		ux.Warn(os.Stderr, "%s", d)
	}
	if len(targets) > 0 {
		targetSource := "auto-detect"
		if opts.TargetFlag != "" {
			targetSource = "--target"
		} else if len(m.Target) > 0 {
			targetSource = "apm.yml"
		}
		ux.Info(os.Stdout, "Targets: %s  (source: %s)", strings.Join(targets, ", "), targetSource)
	}

	deployed, skipped, err := deployMCPEntry(m, opts.TargetFlag, deployDep)
	if err != nil {
		return err
	}

	verb := "Added"
	if status == "replaced" {
		verb = "Replaced"
	}
	if len(skipped) > 0 {
		ux.Info(os.Stdout, "Skipped MCP config for %s  (active targets: %s)",
			strings.Join(skipped, ", "), strings.Join(deployed, ", "))
	}
	// A target adapter can accept the write call yet filter the entry out
	// internally (e.g. a non-https remote URL, or an unresolved placeholder)
	// -- deployed stays empty even though no error was returned. Declaring
	// "Added" in that case would be a false success (found by codex review):
	// apm.yml now carries an entry that is not actually running anywhere.
	if len(deployed) == 0 {
		ux.Warn(os.Stdout, "MCP server %q declared in apm.yml but not deployed to any target; see diagnostics above", opts.Name)
		return nil
	}
	ux.Success(os.Stdout, "%s MCP server %q", verb, opts.Name)
	ux.BulletList(os.Stdout, []ux.Item{
		{Text: fmt.Sprintf("transport: %s", deployDep.Transport)},
		{Text: "apm.yml: apm.yml"},
	})
	return nil
}

// validateMCPConflicts covers the subset of the Python original's E1-E15
// conflict matrix relevant to apm-go's actual flag surface (design.md §2).
func validateMCPConflicts(opts mcpInstallOpts) error {
	if opts.Name == "" {
		return fmt.Errorf("--mcp requires a server name")
	}
	if strings.HasPrefix(opts.Name, "-") {
		return fmt.Errorf("--mcp name cannot start with '-'; did you forget a value for --mcp?")
	}
	if len(opts.PrePackages) > 0 {
		return fmt.Errorf("cannot mix --mcp with positional packages")
	}
	if len(opts.SkillSubset) > 0 {
		return fmt.Errorf("--skill cannot be combined with --mcp")
	}
	if len(opts.HeaderPairs) > 0 && opts.URL == "" {
		return fmt.Errorf("--header requires --url")
	}
	if len(opts.EnvPairs) > 0 && len(opts.Command) == 0 {
		return fmt.Errorf("--env applies to stdio MCP servers; provide a stdio command after --, or use --header for a remote server")
	}
	if opts.URL != "" && len(opts.Command) > 0 {
		return fmt.Errorf("cannot specify both --url and a stdio command")
	}
	if opts.Transport == "stdio" && opts.URL != "" {
		return fmt.Errorf("stdio transport doesn't accept --url")
	}
	if opts.Transport == "stdio" && opts.URL == "" && len(opts.Command) == 0 {
		return fmt.Errorf("stdio transport requires a stdio command after --; registry lookups only resolve remote (http/sse/streamable-http) servers")
	}
	if isRemoteTransport(opts.Transport) && len(opts.Command) > 0 {
		return fmt.Errorf("remote transports don't accept a stdio command")
	}
	if opts.Registry != "" && (opts.URL != "" || len(opts.Command) > 0) {
		return fmt.Errorf("--registry only applies to registry-resolved MCP servers; remove --url or the stdio command, or drop --registry")
	}
	if opts.Version != "" && (opts.URL != "" || len(opts.Command) > 0) {
		return fmt.Errorf("--mcp-version only applies to registry-resolved MCP servers; remove --url or the stdio command, or drop --mcp-version")
	}
	switch opts.Transport {
	case "", "stdio", "http", "sse", "streamable-http":
	default:
		return fmt.Errorf("unknown MCP transport %q", opts.Transport)
	}
	return nil
}

func isRemoteTransport(t string) bool {
	return t == "http" || t == "sse" || t == "streamable-http"
}

// buildPersistEntry computes the apm.yml value for opts WITHOUT making any
// network call, even for a registry-backed entry -- it is safe and cheap to
// call before knowing whether this install will actually change anything.
// Three branches, matching the Python original's build_mcp_entry (design.md
// §4): self-defined stdio, self-defined remote, or registry lookup.
func buildPersistEntry(opts mcpInstallOpts) (*yamllib.Node, error) {
	switch {
	case len(opts.Command) > 0:
		dep, err := buildSelfDefinedStdioDep(opts)
		if err != nil {
			return nil, err
		}
		return mcpEntryNode(dep), nil
	case opts.URL != "":
		dep, err := buildSelfDefinedURLDep(opts)
		if err != nil {
			return nil, err
		}
		return mcpEntryNode(dep), nil
	default:
		return buildRegistryPersistEntryNode(opts, effectiveRegistryURL(opts)), nil
	}
}

// buildDeployDep resolves opts into a fully-resolved *manifest.MCPDependency
// ready to deploy. Only the registry branch is network-bound; self-defined
// branches are rebuilt (cheap, pure, no caching needed) rather than shared
// with buildPersistEntry, keeping the two call sites independent.
func buildDeployDep(opts mcpInstallOpts) (deployDep *manifest.MCPDependency, diags []string, err error) {
	switch {
	case len(opts.Command) > 0:
		dep, err := buildSelfDefinedStdioDep(opts)
		return dep, nil, err
	case opts.URL != "":
		dep, err := buildSelfDefinedURLDep(opts)
		return dep, nil, err
	default:
		return resolveFromRegistry(opts)
	}
}

func buildSelfDefinedStdioDep(opts mcpInstallOpts) (*manifest.MCPDependency, error) {
	dep := &manifest.MCPDependency{Name: opts.Name, Registry: false, Transport: "stdio", Command: opts.Command[0]}
	if len(opts.Command) > 1 {
		args := append([]string{}, opts.Command[1:]...)
		dep.Args = &args
	}
	if len(opts.EnvPairs) > 0 {
		env, err := parseKVPairs(opts.EnvPairs)
		if err != nil {
			return nil, err
		}
		dep.Env = env
	}
	if err := manifest.ValidateMCP(dep); err != nil {
		return nil, err
	}
	return dep, nil
}

func buildSelfDefinedURLDep(opts mcpInstallOpts) (*manifest.MCPDependency, error) {
	transport := opts.Transport
	if transport == "" {
		transport = "http"
	}
	dep := &manifest.MCPDependency{Name: opts.Name, Registry: false, Transport: transport, URL: opts.URL}
	if len(opts.HeaderPairs) > 0 {
		hdrs, err := parseKVPairs(opts.HeaderPairs)
		if err != nil {
			return nil, err
		}
		dep.Headers = hdrs
	}
	if err := manifest.ValidateMCP(dep); err != nil {
		return nil, err
	}
	return dep, nil
}

// effectiveRegistryURL is the registry base URL this call will actually
// query: --registry flag > MCP_REGISTRY_URL env > (empty, meaning the
// client's own default). Shared by buildPersistEntry (to persist which
// registry was used, even when env-selected) and resolveFromRegistry (to
// actually query it), so the two never drift out of sync.
func effectiveRegistryURL(opts mcpInstallOpts) string {
	raw := opts.Registry
	if raw == "" {
		raw = os.Getenv("MCP_REGISTRY_URL")
	}
	if raw == "" {
		return ""
	}
	// Normalize the same way mcpregistry.NewClient does internally (trims a
	// trailing slash), so the value persisted into apm.yml's registry:
	// field matches what NewClient will actually build a Client with --
	// otherwise "--registry https://reg/" then "--registry https://reg"
	// (the same registry to NewClient) compare as different persisted
	// values and force a spurious --force conflict (found by codex review).
	return mcpregistry.NormalizeBaseURL(raw)
}

// resolveFromRegistry looks opts.Name up against the MCP Registry v0.1 API
// and resolves it to a deployable remote (http/sse/streamable-http) MCP
// server. Package-based (npm/docker/pypi/homebrew) stdio resolution is out
// of scope (design.md non-goals) -- a registry hit with no remotes reports
// a clear error naming that gap instead of attempting a partial install.
func resolveFromRegistry(opts mcpInstallOpts) (deployDep *manifest.MCPDependency, diags []string, err error) {
	registryURL := effectiveRegistryURL(opts)
	if registryURL != "" && opts.Registry == "" {
		// Never echo registryURL here: query strings and userinfo are
		// already rejected by NewClient below, but a path-embedded token
		// (e.g. an internal registry using "/t-<token>/" style auth) would
		// still reach this transient stderr diagnostic otherwise (found by
		// codex review). The persisted apm.yml registry: field is a
		// separate, intentional case -- the user explicitly asked to
		// record their custom registry there.
		diags = append(diags, "using a custom MCP registry (from MCP_REGISTRY_URL)")
	}
	client, cerr := mcpregistry.NewClient(registryURL)
	if cerr != nil {
		return nil, nil, fmt.Errorf("--registry: %w", cerr)
	}
	dep, requiredHeaders, rerr := mcpregistry.ResolveDeployable(context.Background(), client, opts.Name, opts.Version, opts.Transport)
	if rerr != nil {
		return nil, nil, rerr
	}
	if len(requiredHeaders) > 0 {
		// Interactively collect the required credentials (TTY only). Entered
		// values are injected into the deploy dep so the server is
		// authenticated, but are NOT persisted to apm.yml -- the entry stays
		// a bare registry reference, keeping secrets out of a committed file.
		// Only when nothing was collected (non-interactive, or left blank) do
		// we fall back to the guidance diagnostic.
		if hdrs := promptRegistryHeaders(requiredHeaders); len(hdrs) > 0 {
			dep.Headers = hdrs
		} else {
			diags = append(diags, fmt.Sprintf(
				"mcp %q requires header(s) %s which apm-go does not resolve automatically; add --header KEY=VALUE if the server needs authentication",
				opts.Name, strings.Join(requiredHeaders, ", ")))
		}
	}

	return dep, diags, nil
}

// buildRegistryPersistEntryNode builds the apm.yml value for a
// registry-resolved entry: it never carries the resolved URL (re-resolved
// fresh on every --mcp call), only enough to repeat the lookup.
// registryURL is the EFFECTIVE registry (flag or env, see
// effectiveRegistryURL) -- persisting it even when only env-selected
// prevents a later run in a different environment (no MCP_REGISTRY_URL set)
// from silently resolving the same declared name against a different,
// possibly unrelated public registry entry (found by codex review).
func buildRegistryPersistEntryNode(opts mcpInstallOpts, registryURL string) *yamllib.Node {
	switch {
	case opts.Version != "":
		pairs := [][2]*yamllib.Node{{strNode("name"), strNode(opts.Name)}, {strNode("version"), strNode(opts.Version)}}
		if opts.Transport != "" {
			pairs = append(pairs, [2]*yamllib.Node{strNode("transport"), strNode(opts.Transport)})
		}
		if registryURL != "" {
			pairs = append(pairs, [2]*yamllib.Node{strNode("registry"), strNode(registryURL)})
		}
		return mapNode(pairs)
	case opts.Transport != "":
		pairs := [][2]*yamllib.Node{{strNode("name"), strNode(opts.Name)}, {strNode("transport"), strNode(opts.Transport)}}
		if registryURL != "" {
			pairs = append(pairs, [2]*yamllib.Node{strNode("registry"), strNode(registryURL)})
		}
		return mapNode(pairs)
	case registryURL != "":
		return mapNode([][2]*yamllib.Node{{strNode("name"), strNode(opts.Name)}, {strNode("registry"), strNode(registryURL)}})
	default:
		return strNode(opts.Name)
	}
}

// mcpEntryNode builds the apm.yml value for a self-defined (registry:false)
// entry -- always a mapping, never the bare-string shorthand.
func mcpEntryNode(dep *manifest.MCPDependency) *yamllib.Node {
	pairs := [][2]*yamllib.Node{
		{strNode("name"), strNode(dep.Name)},
		{strNode("registry"), boolNode(false)},
		{strNode("transport"), strNode(dep.Transport)},
	}
	if dep.Command != "" {
		pairs = append(pairs, [2]*yamllib.Node{strNode("command"), strNode(dep.Command)})
		if dep.Args != nil {
			var items []*yamllib.Node
			for _, a := range *dep.Args {
				items = append(items, strNode(a))
			}
			pairs = append(pairs, [2]*yamllib.Node{strNode("args"), seqNode(items)})
		}
		if len(dep.Env) > 0 {
			pairs = append(pairs, [2]*yamllib.Node{strNode("env"), strMapNode(dep.Env)})
		}
	}
	if dep.URL != "" {
		pairs = append(pairs, [2]*yamllib.Node{strNode("url"), strNode(dep.URL)})
		if len(dep.Headers) > 0 {
			pairs = append(pairs, [2]*yamllib.Node{strNode("headers"), strMapNode(dep.Headers)})
		}
	}
	return mapNode(pairs)
}

// confirmReplaceFunc decides whether to replace an existing MCP entry whose
// value differs from the new one. A nil func means non-interactive: the
// conflict is a hard error telling the user to pass --force. This mirrors the
// Python original's force / interactive-TTY / non-TTY three-way (writer.py
// add_mcp_to_apm_yml): callers inject a TTY-backed prompt only when stdin is
// interactive.
type confirmReplaceFunc func(name string, diff []string) (bool, error)

// upsertMCPEntry inserts or replaces entryNode by name in apm.yml's
// dependencies.mcp sequence (design.md §6): unmatched name -> append
// ("added"); matched name, semantically identical -> no-op ("unchanged");
// matched name, different, force=true -> replace in place ("replaced");
// matched name, different, force=false -> confirm==nil errors (doc
// untouched), else the confirm callback decides: accept -> "replaced",
// decline -> "skipped" (doc untouched, same terminal outcome as unchanged).
//
// On every mutation the sequence's flow style is cleared so an entry appended
// to the empty `mcp: []` that `apm-go init` writes (which go-yaml renders in
// flow style, and whose parsed node inherits FlowStyle) serializes as a
// block-style list, matching the Python original's dump.
func upsertMCPEntry(doc *yamllib.Node, name string, entryNode *yamllib.Node, force bool, confirm confirmReplaceFunc) (status string, err error) {
	root := doc
	if root.Kind == yamllib.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yamllib.MappingNode {
		return "", fmt.Errorf("manifest root is not a mapping")
	}

	depsNode := findOrCreateMappingChild(root, "dependencies")
	mcpSeq := findOrCreateSeqChild(depsNode, "mcp")

	for i, existing := range mcpSeq.Content {
		if mcpEntryName(existing) != name {
			continue
		}
		if nodeValuesEqual(existing, entryNode) {
			return "unchanged", nil
		}
		if !force {
			if confirm == nil {
				return "", fmt.Errorf("MCP server %q already exists in apm.yml. Use --force to replace (non-interactive).", name)
			}
			ok, cerr := confirm(name, diffEntry(existing, entryNode))
			if cerr != nil {
				return "", cerr
			}
			if !ok {
				return "skipped", nil
			}
		}
		mcpSeq.Content[i] = entryNode
		mcpSeq.Style &^= yamllib.FlowStyle
		return "replaced", nil
	}

	mcpSeq.Content = append(mcpSeq.Content, entryNode)
	mcpSeq.Style &^= yamllib.FlowStyle
	return "added", nil
}

// diffEntry renders a short "key: old -> new" list for two differing MCP
// entries (human display for the replace-confirm prompt), mirroring the
// Python original's _diff_entry (writer.py): a bare-string entry is treated
// as {name: value}; two differing bare strings render as a single
// "old -> new" line; a key absent on one side shows "<absent>".
func diffEntry(old, new *yamllib.Node) []string {
	oldStr, oldIsStr := scalarString(old)
	newStr, newIsStr := scalarString(new)
	if oldIsStr && newIsStr {
		if oldStr == newStr {
			return nil
		}
		return []string{fmt.Sprintf("  %s -> %s", oldStr, newStr)}
	}

	oldKeys, oldVals := entryFields(old)
	newKeys, newVals := entryFields(new)
	keys := append([]string{}, oldKeys...)
	for _, k := range newKeys {
		if _, seen := oldVals[k]; !seen {
			keys = append(keys, k)
		}
	}

	var diff []string
	for _, k := range keys {
		ov, ok := oldVals[k]
		if !ok {
			ov = "<absent>"
		}
		nv, ok := newVals[k]
		if !ok {
			nv = "<absent>"
		}
		if ov != nv {
			diff = append(diff, fmt.Sprintf("  %s: %s -> %s", k, ov, nv))
		}
	}
	return diff
}

func scalarString(n *yamllib.Node) (string, bool) {
	if n != nil && n.Kind == yamllib.ScalarNode {
		return n.Value, true
	}
	return "", false
}

// entryFields returns the ordered keys and key->string-repr of a mapping MCP
// entry; a bare scalar entry is treated as {name: value}.
func entryFields(n *yamllib.Node) (keys []string, vals map[string]string) {
	vals = map[string]string{}
	if s, ok := scalarString(n); ok {
		return []string{"name"}, map[string]string{"name": s}
	}
	if n != nil && n.Kind == yamllib.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			keys = append(keys, k)
			vals[k] = fmt.Sprintf("%v", nodeToValue(n.Content[i+1]))
		}
	}
	return keys, vals
}

func mcpEntryName(n *yamllib.Node) string {
	if n.Kind == yamllib.ScalarNode {
		return n.Value
	}
	if n.Kind == yamllib.MappingNode {
		for i := 0; i < len(n.Content)-1; i += 2 {
			if n.Content[i].Value == "name" {
				return n.Content[i+1].Value
			}
		}
	}
	return ""
}

func nodeValuesEqual(a, b *yamllib.Node) bool {
	return reflect.DeepEqual(nodeToValue(a), nodeToValue(b))
}

// nodeToValue normalizes a yaml.Node subtree into a plain Go value for
// equality comparison, respecting !!bool so a freshly-built node and one
// re-parsed from disk compare equal regardless of provenance.
func nodeToValue(n *yamllib.Node) any {
	switch n.Kind {
	case yamllib.ScalarNode:
		if n.Tag == "!!bool" {
			return n.Value == "true"
		}
		return n.Value
	case yamllib.MappingNode:
		m := map[string]any{}
		for i := 0; i < len(n.Content)-1; i += 2 {
			m[n.Content[i].Value] = nodeToValue(n.Content[i+1])
		}
		return m
	case yamllib.SequenceNode:
		s := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			s = append(s, nodeToValue(c))
		}
		return s
	}
	return nil
}

// deployMCPEntry writes dep's config to every active target that supports
// MCP, reusing the existing per-target writers (internal/deploy/mcp_*.go)
// unmodified -- this is not a new deploy path, just a single-Primitive call
// into the same one regular `apm install` uses.
func deployMCPEntry(m *manifest.Manifest, targetFlag string, dep *manifest.MCPDependency) (deployedTargets, skippedTargets []string, err error) {
	targets, targetDiags := deploy.ResolveTargets(targetFlag, m.Target, ".")
	for _, d := range targetDiags {
		ux.Warn(os.Stderr, "%s", d)
	}

	prims := []deploy.Primitive{{Name: dep.Name, Type: deploy.TypeMCP, Source: "local", MCP: dep}}
	for _, t := range targets {
		adapter, ok := deploy.Adapters[t]
		if !ok {
			continue
		}
		mcpAdapter, ok := adapter.(deploy.MCPTarget)
		if !ok {
			skippedTargets = append(skippedTargets, t)
			continue
		}
		_, written, diags, werr := mcpAdapter.WriteMCP(prims, ".")
		for _, d := range diags {
			ux.Warn(os.Stderr, "%s", d)
		}
		if werr != nil {
			return deployedTargets, skippedTargets, fmt.Errorf("deploy to %s: %w", t, werr)
		}
		if len(written) > 0 {
			deployedTargets = append(deployedTargets, t)
		}
	}
	return deployedTargets, skippedTargets, nil
}

// ── small yaml.Node / map builders, shared by the functions above ──

func strNode(s string) *yamllib.Node {
	return &yamllib.Node{Kind: yamllib.ScalarNode, Value: s, Tag: "!!str"}
}

func boolNode(b bool) *yamllib.Node {
	v := "false"
	if b {
		v = "true"
	}
	return &yamllib.Node{Kind: yamllib.ScalarNode, Value: v, Tag: "!!bool"}
}

func mapNode(pairs [][2]*yamllib.Node) *yamllib.Node {
	n := &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map"}
	for _, p := range pairs {
		n.Content = append(n.Content, p[0], p[1])
	}
	return n
}

func seqNode(items []*yamllib.Node) *yamllib.Node {
	n := &yamllib.Node{Kind: yamllib.SequenceNode, Tag: "!!seq"}
	n.Content = append(n.Content, items...)
	return n
}

func strMapNode(m map[string]string) *yamllib.Node {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs [][2]*yamllib.Node
	for _, k := range keys {
		pairs = append(pairs, [2]*yamllib.Node{strNode(k), strNode(m[k])})
	}
	return mapNode(pairs)
}

func findOrCreateMappingChild(parent *yamllib.Node, key string) *yamllib.Node {
	for i := 0; i < len(parent.Content)-1; i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1]
		}
	}
	child := &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, strNode(key), child)
	return child
}

func findOrCreateSeqChild(parent *yamllib.Node, key string) *yamllib.Node {
	for i := 0; i < len(parent.Content)-1; i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1]
		}
	}
	child := &yamllib.Node{Kind: yamllib.SequenceNode, Tag: "!!seq"}
	parent.Content = append(parent.Content, strNode(key), child)
	return child
}

func parseKVPairs(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		idx := strings.Index(p, "=")
		if idx <= 0 {
			// Never echo p: this parses both --env and --header, so a
			// mistyped separator (e.g. --header "Authorization: Bearer
			// secret" using ":" instead of "=") would otherwise leak the
			// value straight into this error message (found by codex
			// review).
			return nil, fmt.Errorf("invalid KEY=VALUE pair: missing '=' (or empty key)")
		}
		out[p[:idx]] = p[idx+1:]
	}
	return out, nil
}
