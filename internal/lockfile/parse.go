package lockfile

import (
	"fmt"

	"go.yaml.in/yaml/v4"
)

// ParseLockfile parses a validated yaml.Node into a Lockfile.
// The node must have been loaded via SafeLoad.
func ParseLockfile(doc *yaml.Node) (*Lockfile, error) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("lockfile: expected document node")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("lockfile: top-level must be a mapping")
	}

	lf := &Lockfile{}
	for i := 0; i < len(root.Content)-1; i += 2 {
		key := root.Content[i].Value
		val := root.Content[i+1]

		switch key {
		case "lockfile_version":
			lf.Version = val.Value
		case "generated_at":
			lf.GeneratedAt = val.Value
		case "apm_version":
			lf.APMVersion = val.Value
		case "dependencies":
			if val.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("lockfile: dependencies must be a sequence")
			}
			for j, entry := range val.Content {
				dep, err := parseLockedDep(entry, j)
				if err != nil {
					return nil, err
				}
				lf.Dependencies = append(lf.Dependencies, *dep)
			}
		}
	}

	if lf.Version == "" {
		return nil, fmt.Errorf("lockfile: missing lockfile_version")
	}
	if lf.Version != "1" && lf.Version != "2" {
		return nil, fmt.Errorf("lockfile: unknown lockfile_version %q; upgrade your tool or regenerate from manifest", lf.Version)
	}

	return lf, nil
}

func parseLockedDep(node *yaml.Node, idx int) (*LockedDep, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("lockfile: dependency %d must be a mapping", idx)
	}

	d := &LockedDep{}
	for i := 0; i < len(node.Content)-1; i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "repo_url":
			d.RepoURL = val.Value
		case "virtual_path":
			d.VirtualPath = val.Value
		case "source":
			d.Source = val.Value
		case "constraint":
			d.Constraint = val.Value
		case "resolved_tag":
			d.ResolvedTag = val.Value
		case "resolved_commit":
			d.ResolvedCommit = val.Value
		case "resolved_ref":
			d.ResolvedRef = val.Value
		case "resolved_url":
			d.ResolvedURL = val.Value
		case "resolved_hash":
			d.ResolvedHash = val.Value
		case "resolved_by":
			d.ResolvedBy = val.Value
		case "resolved_at":
			d.ResolvedAt = val.Value
		case "version":
			d.Version = val.Value
		case "depth":
			if val.Tag == "!!int" || (val.Value != "" && val.Value[0] >= '0' && val.Value[0] <= '9') {
				n := 0
				for _, c := range val.Value {
					n = n*10 + int(c-'0')
				}
				d.Depth = n
			}
		case "tree_sha256":
			d.TreeSHA256 = val.Value
		case "deployed_files":
			if val.Kind == yaml.SequenceNode {
				for _, f := range val.Content {
					d.DeployedFiles = append(d.DeployedFiles, f.Value)
				}
			}
		case "deployed_file_hashes":
			if val.Kind == yaml.MappingNode {
				d.DeployedHashes = make(map[string]string)
				for j := 0; j < len(val.Content)-1; j += 2 {
					d.DeployedHashes[val.Content[j].Value] = val.Content[j+1].Value
				}
			}
		}
	}

	return d, nil
}
