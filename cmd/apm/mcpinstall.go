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

	status, err := upsertMCPEntry(node, opts.Name, entryNode, opts.Force)
	if err != nil {
		return err
	}
	if status == "unchanged" {
		// A stale or missing deployed target file for an unchanged entry is
		// a pre-existing limitation of --mcp itself (present in the Python
		// original too, per source reading during design): re-run `apm
		// install` (the full pipeline) or delete+re-add the entry to force
		// redeployment.
		fmt.Printf("[i] MCP server %q unchanged\n", opts.Name)
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
		fmt.Fprintf(os.Stderr, "[!] %s\n", d)
	}

	manifestBytes, err := yamlcore.SafeDump(node)
	if err != nil {
		return fmt.Errorf("serialize apm.yml: %w", err)
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
		fmt.Fprintln(os.Stderr, d)
	}
	if len(targets) > 0 {
		targetSource := "auto-detect"
		if opts.TargetFlag != "" {
			targetSource = "--target"
		} else if len(m.Target) > 0 {
			targetSource = "apm.yml"
		}
		fmt.Printf("[i] Targets: %s  (source: %s)\n", strings.Join(targets, ", "), targetSource)
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
		fmt.Printf("[i] Skipped MCP config for %s  (active targets: %s)\n",
			strings.Join(skipped, ", "), strings.Join(deployed, ", "))
	}
	// A target adapter can accept the write call yet filter the entry out
	// internally (e.g. a non-https remote URL, or an unresolved placeholder)
	// -- deployed stays empty even though no error was returned. Declaring
	// "Added" in that case would be a false success (found by codex review):
	// apm.yml now carries an entry that is not actually running anywhere.
	if len(deployed) == 0 {
		fmt.Printf("[!] MCP server %q declared in apm.yml but not deployed to any target; see diagnostics above\n", opts.Name)
		return nil
	}
	fmt.Printf("[+] %s MCP server %q\n", verb, opts.Name)
	fmt.Printf("  transport: %s\n", deployDep.Transport)
	fmt.Printf("  apm.yml: apm.yml\n")
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
	info, ferr := client.FindServerByReference(context.Background(), opts.Name, opts.Version)
	if ferr != nil {
		return nil, nil, fmt.Errorf("mcp registry: %w", ferr)
	}
	if info == nil {
		return nil, nil, fmt.Errorf("MCP server %q not found in registry", opts.Name)
	}
	if len(info.Remotes) == 0 {
		if info.HasPackages {
			return nil, nil, fmt.Errorf(
				"MCP server %q only provides package-based (stdio) installation, which apm-go does not yet support; declare it manually with a stdio command after --", opts.Name)
		}
		return nil, nil, fmt.Errorf("MCP server %q has no deployable remote endpoint", opts.Name)
	}

	remote := info.Remotes[0]
	transport := remote.TransportType
	if transport == "" {
		transport = "http"
	}
	if !isRemoteTransport(transport) {
		return nil, nil, fmt.Errorf("MCP server %q: unsupported remote transport %q", opts.Name, transport)
	}
	if opts.Transport != "" {
		// Defense in depth: validateMCPConflicts already rejects
		// Transport=="stdio" with neither --url nor a stdio command (the
		// only way to reach this registry branch), but resolveFromRegistry
		// is also called directly by tests -- an override to a non-remote
		// transport here would silently build a stdio dep with a URL and
		// no command, which writers would deploy as broken (codex review).
		if !isRemoteTransport(opts.Transport) {
			return nil, nil, fmt.Errorf("--transport %q is not valid for a registry-resolved server; only http, sse, or streamable-http apply", opts.Transport)
		}
		transport = opts.Transport
	}

	// The deployed server's Name (used both as the target config's server
	// key and as the identity upsertMCPEntry checked for conflicts) must be
	// opts.Name, not a shortened slug of the registry's canonical name
	// (e.g. "io.github.github/github-mcp-server" -> "github-mcp-server").
	// A derived short name is a DIFFERENT identity than what upsertMCPEntry
	// just checked apm.yml for -- it could silently overwrite an unrelated
	// existing target config entry that happens to slug-collide, bypassing
	// --force entirely (found by codex review). Using opts.Name verbatim
	// keeps persist-identity and deploy-identity the same value.
	dep := &manifest.MCPDependency{Name: opts.Name, Registry: false, Transport: transport, URL: remote.URL}

	// Registry-supplied URLs are not trusted input any more than a
	// self-defined --url: a compromised or malicious registry entry could
	// return a URL with embedded credentials, which would otherwise be
	// deployed straight into the target's MCP config file. ValidateMCP's
	// self-defined checks (Registry==false, which dep already has) run
	// unconditionally here, reusing the same credential guard instead of
	// duplicating it (found by codex review -- this path bypassed the
	// self-defined --url guard entirely).
	if verr := manifest.ValidateMCP(dep); verr != nil {
		return nil, nil, fmt.Errorf("MCP server %q: registry returned an invalid entry: %w", opts.Name, verr)
	}

	if len(remote.RequiredHeaders) > 0 {
		diags = append(diags, fmt.Sprintf(
			"mcp %q requires header(s) %s which apm-go does not resolve automatically; add --header KEY=VALUE if the server needs authentication",
			opts.Name, strings.Join(remote.RequiredHeaders, ", ")))
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

// upsertMCPEntry inserts or replaces entryNode by name in apm.yml's
// dependencies.mcp sequence (design.md §6): unmatched name -> append
// ("added"); matched name, semantically identical -> no-op ("unchanged");
// matched name, different, force=false -> error, doc untouched; matched
// name, different, force=true -> replace in place ("replaced").
func upsertMCPEntry(doc *yamllib.Node, name string, entryNode *yamllib.Node, force bool) (status string, err error) {
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
			return "", fmt.Errorf("MCP server %q already exists in apm.yml. Use --force to replace.", name)
		}
		mcpSeq.Content[i] = entryNode
		return "replaced", nil
	}

	mcpSeq.Content = append(mcpSeq.Content, entryNode)
	return "added", nil
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
		fmt.Fprintln(os.Stderr, d)
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
			fmt.Fprintf(os.Stderr, "[!] %s\n", d)
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
