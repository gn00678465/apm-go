package main

import (
	"os"
	"sort"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/ux"
)

// staleMCPRemoval is the servers to take out of one target's MCP config.
type staleMCPRemoval struct {
	target string
	names  []string
}

// staleMCPRemovals compares the old lock with the MCP servers declared for
// this run (deploy.DeployResult.DeclaredMCPServers) and lists, in the order
// they are printed, the servers apm-go wrote before that are no longer
// declared or no longer go to a target: the Oracle's
// install/mcp/integration.py:256-370, whose new set is the declared servers
// (:297). A server is declared whatever became of it after: a registry
// lookup that failed or a write that failed does not make it stale.
//
//   - With servers declared: every server the old mcp_target_servers lists
//     for a target that is no longer active, then every server of the old
//     mcp_servers that is no longer declared, for every active target.
//   - With none: every server the old mcp_target_servers lists for a target,
//     also an active one, that the old mcp_servers has.
//
// A lock with no mcp_target_servers key (apm-go v0.3.0) gives the first
// list nothing, as the Oracle's legacy adoption gives it nothing for a lock
// with no mcp_configs (install/mcp/ownership.py:47-110).
//
// Deviation: a recorded target that is not one of apm-go's MCP targets
// (the Oracle's cursor) is skipped, since apm-go has no writer for its file,
// and a name is removed only as written: apm-go keys every config by the
// full name, so the Oracle's extra removal of the part after the last "/"
// would only reach an entry apm-go did not write.
func staleMCPRemovals(targets, declared []string, existingLock *lockfile.Lockfile) []staleMCPRemoval {
	var active []string
	isActive := map[string]bool{}
	for _, t := range targets {
		if deploy.MCPConfigLabel(t) != "" && !isActive[t] {
			isActive[t] = true
			active = append(active, t)
		}
	}
	sort.Strings(active)
	recorded := make([]string, 0, len(existingLock.MCPTargetServers))
	for t := range existingLock.MCPTargetServers {
		if deploy.MCPConfigLabel(t) != "" {
			recorded = append(recorded, t)
		}
	}
	sort.Strings(recorded)

	var out []staleMCPRemoval
	add := func(target string, names []string) {
		if len(names) > 0 {
			out = append(out, staleMCPRemoval{target: target, names: names})
		}
	}
	if len(declared) == 0 {
		old := stringSet(existingLock.MCPServers)
		for _, t := range recorded {
			var names []string
			for _, name := range existingLock.MCPTargetServers[t] {
				if old[name] {
					names = append(names, name)
				}
			}
			add(t, names)
		}
		return out
	}
	for _, t := range recorded {
		if !isActive[t] {
			add(t, existingLock.MCPTargetServers[t])
		}
	}
	kept := stringSet(declared)
	var stale []string
	for _, name := range existingLock.MCPServers {
		if !kept[name] {
			stale = append(stale, name)
		}
	}
	for _, t := range active {
		add(t, stale)
	}
	return out
}

func stringSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// cleanStaleMCPServers removes from the MCP config files under root the
// servers staleMCPRemovals lists, and prints a line for each server removed.
// The lock that follows does not record them, so a failure is the install's
// failure and the lock is not written.
//
// Deviation: the Oracle cleans .mcp.json only when .claude/ is a directory
// and opencode.json only when .opencode/ is one; apm-go writes
// opencode.json without making .opencode/, so it cleans both files whatever
// the directories.
func cleanStaleMCPServers(root string, targets, declared []string, existingLock *lockfile.Lockfile, userScope bool) error {
	if existingLock == nil {
		return nil
	}
	for _, r := range staleMCPRemovals(targets, declared, existingLock) {
		removed, err := deploy.RemoveStaleMCPServers(root, r.target, r.names, userScope)
		if err != nil {
			return err
		}
		label := deploy.MCPConfigLabel(r.target)
		for _, name := range removed {
			// The Oracle prints its TOML and Copilot cleanups as success
			// lines and the others as info
			// (integration/mcp_integrator.py:125-349).
			if r.target == "codex" || r.target == "copilot" {
				ux.Success(os.Stdout, "Removed stale MCP server '%s' from %s", name, label)
			} else {
				ux.Info(os.Stdout, "Removed stale MCP server '%s' from %s", name, label)
			}
		}
	}
	return nil
}
