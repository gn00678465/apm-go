package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/ux"
	yamllib "go.yaml.in/yaml/v4"
)

// mcpConfigNode is one mcp_configs value of the lock: the server's fields in
// the order of the Oracle's MCPDependency.to_dict
// (models/dependency/mcp.py:254). package, tools and cwd, which apm-go does
// not model, are absent. enabled is recorded for every target, not only
// OpenCode, as the Oracle records it.
func mcpConfigNode(dep *manifest.MCPDependency) *yamllib.Node {
	pairs := [][2]*yamllib.Node{{strNode("name"), strNode(dep.Name)}}
	add := func(key string, val *yamllib.Node) {
		pairs = append(pairs, [2]*yamllib.Node{strNode(key), val})
	}
	if dep.Transport != "" {
		add("transport", strNode(dep.Transport))
	}
	if dep.Env != nil {
		add("env", orderedStrMapNode(dep.Env, dep.EnvOrder))
	}
	if dep.Args != nil {
		items := make([]*yamllib.Node, 0, len(*dep.Args))
		for _, a := range *dep.Args {
			items = append(items, strNode(a))
		}
		args := seqNode(items)
		if len(items) == 0 {
			args.Style = yamllib.FlowStyle
		}
		add("args", args)
	}
	if dep.Version != "" {
		add("version", strNode(dep.Version))
	}
	switch reg := dep.Registry.(type) {
	case bool:
		add("registry", boolNode(reg))
	case string:
		add("registry", strNode(reg))
	}
	if dep.Headers != nil {
		add("headers", orderedStrMapNode(dep.Headers, dep.HeaderOrder))
	}
	if dep.URL != "" {
		add("url", strNode(dep.URL))
	}
	if dep.Command != "" {
		add("command", strNode(dep.Command))
	}
	if dep.Enabled != nil {
		add("enabled", dep.Enabled)
	}
	return mapNode(pairs)
}

// manifestMCPOrder lists the deployed servers in the order apm.yml declares
// them, then any a dependency brought, by name.
func manifestMCPOrder(m *manifest.Manifest, deployed []string, targetsByServer map[string]map[string]bool) []string {
	var order []string
	seen := map[string]bool{}
	for _, list := range [][]*manifest.MCPDependency{m.MCPServers, m.MCPDevServers} {
		for _, dep := range list {
			if _, ok := targetsByServer[dep.Name]; ok && !seen[dep.Name] {
				seen[dep.Name] = true
				order = append(order, dep.Name)
			}
		}
	}
	for _, server := range deployed {
		if !seen[server] {
			order = append(order, server)
		}
	}
	return order
}

// ensureClaudeDir creates .claude under root: the Oracle leaves that
// directory, empty, when it configures an MCP server for Claude.
func ensureClaudeDir(root string, targets map[string]bool) {
	if !targets["claude"] {
		return
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		ux.Warn(os.Stderr, "could not create .claude: %v", err)
	}
}

// manifestMCPByName indexes the MCP servers apm.yml declares.
func manifestMCPByName(m *manifest.Manifest) map[string]*manifest.MCPDependency {
	byName := make(map[string]*manifest.MCPDependency)
	for _, list := range [][]*manifest.MCPDependency{m.MCPServers, m.MCPDevServers} {
		for _, dep := range list {
			if _, ok := byName[dep.Name]; !ok {
				byName[dep.Name] = dep
			}
		}
	}
	return byName
}

// recordMCPState fills the lock's mcp_configs and mcp_target_servers from the
// servers this run wrote for each target. lock.MCPServers is already set. A
// server a dependency declared has no entry in apm.yml, so it gets no config.
func recordMCPState(lock *lockfile.Lockfile, declared map[string]*manifest.MCPDependency, targetsByServer map[string]map[string]bool) {
	lock.MCPConfigs = make(map[string]*yamllib.Node)
	lock.MCPTargetServers = make(map[string][]string)
	for _, server := range lock.MCPServers {
		if dep, ok := declared[server]; ok {
			lock.MCPConfigs[server] = mcpConfigNode(dep)
		}
		for target := range targetsByServer[server] {
			lock.MCPTargetServers[target] = append(lock.MCPTargetServers[target], server)
		}
	}
	for target := range lock.MCPTargetServers {
		sort.Strings(lock.MCPTargetServers[target])
	}
}

// printMCPBlock prints the MCP section of an install the way the Oracle's
// console does (integration/mcp_integrator_install.py:825, :950-1081, :1175)
// and returns how many servers this run configured or updated. A server is
// already configured when the old lock holds the same config for it and
// lists it for every target.
func printMCPBlock(order []string, declared map[string]*manifest.MCPDependency, newLock, existingLock *lockfile.Lockfile, targetsByServer map[string]map[string]bool, explicitTarget, declaredTarget bool) int {

	targetSet := map[string]bool{}
	for _, set := range targetsByServer {
		for t := range set {
			targetSet[t] = true
		}
	}
	targets := sortedStringSet(targetSet)

	ux.Plain(os.Stdout, "+- MCP Servers (%d)", len(order))
	switch {
	case explicitTarget:
		ux.Info(os.Stdout, "Targeting specific %s: %s", pluralWord(len(targets), "runtime", "runtimes"), strings.Join(targets, ", "))
	case declaredTarget:
		ux.Info(os.Stdout, "Targeting declared %s from apm.yml: %s", pluralWord(len(targets), "target", "targets"), strings.Join(targets, ", "))
	}

	state := func(server string) (configured, known bool) {
		if existingLock == nil {
			return false, false
		}
		old, ok := existingLock.MCPConfigs[server]
		if !ok {
			return false, false
		}
		if !lockfile.MCPConfigEqual(old, newLock.MCPConfigs[server]) {
			return false, true
		}
		for t := range targetsByServer[server] {
			if !containsString(existingLock.MCPTargetServers[t], server) {
				return false, false
			}
		}
		return true, true
	}

	var pending []string
	for _, server := range order {
		if configured, _ := state(server); configured {
			ux.SuccessAt(os.Stdout, "|  ", "%s (already configured)", server)
		} else {
			pending = append(pending, server)
		}
	}

	configuredCount, updatedCount := 0, 0
	for _, server := range pending {
		_, isUpdate := state(server)
		label := ""
		if dep, ok := declared[server]; ok && dep.Registry == false {
			// A self-defined server always has a transport (manifest.ValidateMCP).
			label = " (self-defined, " + dep.Transport + ")"
		}
		serverTargets := sortedStringSet(targetsByServer[server])
		titles := make([]string, len(serverTargets))
		for i, t := range serverTargets {
			titles[i] = titleCase(t)
		}
		action, done := "Configuring", "configured"
		if isUpdate {
			action, done = "Updating", "updated"
			updatedCount++
		} else {
			configuredCount++
		}
		ux.ProgressAt(os.Stdout, "|  ", " %s%s", server, label)
		ux.Plain(os.Stdout, "|     +- %s for %s...", action, strings.Join(titles, ", "))
		for _, t := range serverTargets {
			// The Claude adapter's own two lines (adapters/client/claude.py:304).
			// The lines of the other adapters were not compared against the
			// Oracle and are not printed.
			if t == "claude" {
				ux.Plain(os.Stdout, "Successfully configured MCP server '%s' for Claude Code", server)
				ux.Plain(os.Stdout, "  + %s", server)
			}
		}
		ux.SuccessAt(os.Stdout, "|  ", " %s -> %s (%s)", server, strings.Join(titles, ", "), done)
	}

	var parts []string
	if configuredCount > 0 {
		parts = append(parts, "configured "+countNoun(configuredCount, "server"))
	}
	if updatedCount > 0 {
		parts = append(parts, "updated "+countNoun(updatedCount, "server"))
	}
	if len(parts) == 0 {
		ux.Success(os.Stdout, "All servers up to date")
		return 0
	}
	summary := strings.Join(parts, ", ")
	ux.Success(os.Stdout, "%s", strings.ToUpper(summary[:1])+summary[1:])
	return configuredCount + updatedCount
}

func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// titleCase is Python's str.title for the name of a target that takes MCP
// servers: each is one lower-case ASCII word, so only its first letter
// changes. A target name with a separator needs the full str.title rule.
func titleCase(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}

// orderedStrMapNode is a YAML mapping of m with its keys in declared order.
func orderedStrMapNode(m map[string]string, order []string) *yamllib.Node {
	var pairs [][2]*yamllib.Node
	for _, k := range manifest.OrderedKeys(m, order) {
		pairs = append(pairs, [2]*yamllib.Node{strNode(k), strNode(m[k])})
	}
	return mapNode(pairs)
}

// kvPairKeys returns the keys of KEY=VALUE flags in the order given.
func kvPairKeys(pairs []string) []string {
	keys := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if key, _, ok := strings.Cut(p, "="); ok {
			keys = append(keys, key)
		}
	}
	return keys
}

// setEntryMapOrder puts the pairs of the env or headers mapping of an apm.yml
// MCP entry into the given key order. The mapping is built from the same
// KEY=VALUE flags as order, so order names every key of it.
func setEntryMapOrder(entry *yamllib.Node, key string, order []string) {
	for i := 0; i+1 < len(entry.Content); i += 2 {
		m := entry.Content[i+1]
		if entry.Content[i].Value != key || m.Kind != yamllib.MappingNode {
			continue
		}
		var ordered []*yamllib.Node
		used := make([]bool, len(m.Content)/2)
		for _, k := range order {
			for j := 0; j+1 < len(m.Content); j += 2 {
				if !used[j/2] && m.Content[j].Value == k {
					used[j/2] = true
					ordered = append(ordered, m.Content[j], m.Content[j+1])
					break
				}
			}
		}
		m.Content = ordered
	}
}
