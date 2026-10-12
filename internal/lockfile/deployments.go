package lockfile

import (
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

// selfOwner is the owner key of the project's own content.
const selfOwner = "."

// legacyTargetPrefixes maps the first directory of a deployed path to the
// target that owns it (the Oracle's _LEGACY_TARGET_PREFIXES and
// _LEGACY_USER_TARGET_PREFIXES, core/deployment_ledger.py:29-41). A path
// under a user prefix has scope "user".
var (
	legacyTargetPrefixes = []struct{ prefix, target string }{
		{".github/", "copilot"},
		{".claude/", "claude"},
		{".cursor/", "cursor"},
		{".windsurf/", "windsurf"},
		{".kiro/", "kiro"},
		{".gemini/", "gemini"},
		{".codex/", "codex"},
		{".opencode/", "opencode"},
	}
	legacyUserTargetPrefixes = []struct{ prefix, target string }{
		{".copilot/", "copilot"},
	}
)

type deploymentRecord struct {
	kind, target, value, runtime, scope string
	owners                              []string
	activeOwner, contentHash            string
}

func (r *deploymentRecord) key() string {
	return r.target + "|" + r.runtime + "|" + r.scope + "|" + r.value
}

// ownerKey is the key the Oracle's deployment ledger names a dependency by
// (build_dependency_unique_key, models/dependency/identity.py:184). It is
// not UniqueKey: a local package is named by its declared path. The Oracle
// also lower-cases a GitHub owner/repo, which apm-go does not.
func (d *LockedDep) ownerKey() string {
	if d.Source == "local" && d.LocalPath != "" {
		return d.LocalPath
	}
	key := d.UniqueKey()
	if d.RegistryPrefix != "" {
		return key
	}
	if host := strings.ToLower(strings.TrimSpace(d.Host)); host != "" && host != "github.com" {
		return host + "/" + key
	}
	return key
}

// deploymentRows derives the lock's deployments section from the deployed
// files of each dependency, the project's own deployed files and the MCP
// servers of each target, the way the Oracle synthesizes it from the same
// views (DeploymentLedgerCodec.from_lockfile and rows,
// core/deployment_ledger.py:232, :656). The target of a file comes from
// PathTargets when install or the parsed lock named it, else from the path
// prefix.
func deploymentRows(lf *Lockfile) *yaml.Node {
	records := make(map[string]*deploymentRecord)
	claim := func(owner string, paths []string, hashes map[string]string) {
		for _, value := range paths {
			rec := legacyPathRecord(value)
			if target := lf.PathTargets[value]; target != "" && rec.kind == "project-relative" {
				rec.target = target
			}
			if prior, ok := records[rec.key()]; ok {
				rec.owners = withoutString(prior.owners, owner)
				rec.contentHash = prior.contentHash
			}
			rec.owners = append(rec.owners, owner)
			rec.activeOwner = owner
			if h := hashes[value]; h != "" {
				rec.contentHash = h
			}
			records[rec.key()] = rec
		}
	}
	for i := range lf.Dependencies {
		dep := &lf.Dependencies[i]
		claim(dep.ownerKey(), dep.DeployedFiles, dep.DeployedHashes)
	}
	claim(selfOwner, lf.LocalDeployedFiles, lf.LocalDeployedHashes)
	for runtime, servers := range lf.MCPTargetServers {
		for _, server := range servers {
			rec := &deploymentRecord{
				kind: "uri", target: "mcp", value: server, runtime: runtime, scope: "project",
				owners: []string{selfOwner}, activeOwner: selfOwner,
			}
			records[rec.key()] = rec
		}
	}

	keys := make([]string, 0, len(records))
	for k := range records {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, k := range keys {
		rec := records[k]
		row := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		addScalar(row, "kind", rec.kind)
		addScalar(row, "target", rec.target)
		addScalar(row, "value", rec.value)
		addNode(row, "runtime", scalarOrNull(rec.runtime))
		addScalar(row, "scope", rec.scope)
		addNode(row, "owners", flowIfEmpty(stringSeq(rec.owners)))
		addScalar(row, "active_owner", rec.activeOwner)
		addNode(row, "content_hash", scalarOrNull(rec.contentHash))
		seq.Content = append(seq.Content, row)
	}
	return seq
}

func legacyPathRecord(value string) *deploymentRecord {
	rec := &deploymentRecord{kind: "project-relative", value: value, scope: "project"}
	if scheme, _, ok := strings.Cut(value, "://"); ok {
		rec.kind = "uri"
		rec.target = strings.TrimSuffix(scheme, "-db")
		return rec
	}
	for _, p := range legacyUserTargetPrefixes {
		if strings.HasPrefix(value, p.prefix) {
			rec.target, rec.scope = p.target, "user"
			return rec
		}
	}
	rec.target = "legacy"
	for _, p := range legacyTargetPrefixes {
		if strings.HasPrefix(value, p.prefix) {
			rec.target = p.target
			break
		}
	}
	return rec
}

func scalarOrNull(value string) *yaml.Node {
	if value == "" {
		return nullNode()
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"}
}

func withoutString(items []string, drop string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}
