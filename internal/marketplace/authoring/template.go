// This file (template.go) holds `apm marketplace init`'s scaffold text
// (mkt-040): raw, hand-formatted YAML strings, not values run through
// yaml.Marshal -- init needs to reproduce exact comments and layout that a
// Marshal round-trip cannot, and the cmd layer (cmd/apm/marketplace_authoring.go)
// splices this text onto apm.yml surgically (append, or a
// yamlcore.PatchMappingPath value-span replace under --force), never by
// re-encoding the whole file.
package authoring

import "strings"

// initMinimalApmYMLShell is the placeholder apm.yml `apm marketplace init`
// writes first when no apm.yml exists yet in the current directory, so
// there is somewhere to append the marketplace: block below. Mirrors
// Python apm's commands/marketplace/init.py inline scaffold_text.
const initMinimalApmYMLShell = "name: {{NAME}}\n" +
	"version: 0.1.0\n" +
	"description: A short description of what this repo offers\n"

// RenderMinimalApmYMLShell renders the placeholder apm.yml content for
// `apm marketplace init` to write when apm.yml does not already exist.
// name falls back to "my-marketplace" when empty (Python apm's default).
func RenderMinimalApmYMLShell(name string) string {
	if name == "" {
		name = "my-marketplace"
	}
	return strings.ReplaceAll(initMinimalApmYMLShell, "{{NAME}}", name)
}

// initBlockTemplate is the apm.yml `marketplace:` block scaffold (mkt-040).
//
// Deliberately deviates from Python apm's render_marketplace_block: the
// pinned-package example comment below uses "ref: v1.0.0" instead of
// upstream's "ref: main" (mkt-040 修訂版, marketplace-checklist.md).
// `apm pack` rejects branch/HEAD refs with HeadNotAllowedError and exposes
// no allow-head escape hatch (checklist mkt-055), so a scaffold suggesting
// "ref: main" would walk every new user straight into a pack failure the
// moment they uncomment that example.
const initBlockTemplate = `# Marketplace authoring config (APM-only).
# Run 'apm-go pack' to compile this block to .claude-plugin/marketplace.json.
# Optionally enable Codex output below to also write .agents/plugins/marketplace.json.
#
# Top-level 'name', 'description', and 'version' are inherited from
# the project (above) by default.  Override them inside this block when
# the marketplace is published independently of the project's release
# cadence.
#
# For the full schema, see:
#   https://microsoft.github.io/apm/guides/marketplace-authoring/
marketplace:
  owner:
    name: {{OWNER}}
    url: https://github.com/{{OWNER}}

  # Default tag pattern used to resolve version ranges for each package.
  build:
    tagPattern: "v{version}"

  # Output targets (map form). 'claude' is enabled by default;
  # uncomment 'codex' below to publish the Codex artifact too.
  # Each output writes to its profile default path; add 'path:'
  # under a key to override.
  outputs:
    claude: {}
    # codex: {}
    #
    # Note: enabling codex requires every package below to declare
    # 'category:' (e.g. category: Productivity).

  # CI tip: build one or all formats with a machine-readable manifest:
  #   apm-go pack --marketplace=claude,codex --json | jq -r '.marketplace.outputs[].path'

  packages:
    - name: example-package
      description: Human-readable description of the package
      source: {{OWNER}}/example-package
      version: "^1.0.0"
      # Required when outputs includes codex:
      # category: Productivity
      # Optional overrides:
      # subdir: path/inside/repo
      # Per-package version tag (recommended for monorepos so each
      # package can be released independently). Leave commented for
      # repo-wide lockstep tagging.
      # tag_pattern: "{name}-v{version}"
      # include_prerelease: false
      # ref: v1.0.0  # pin to an explicit ref instead of a version range

    # Local-path entry: ship a package shipped alongside this repo.
    # - name: local-tool
    #   source: ./packages/local-tool
    #   description: A locally vendored tool
    #   version: 0.1.0
    #   # tag_pattern: "{name}-v{version}"
`

// RenderInitBlock renders the marketplace: block scaffold for the given
// owner (falling back to "acme-org" when owner is empty, matching Python
// apm's render_marketplace_block default). name/description/version are
// deliberately absent from this block: they are inherited from apm.yml's
// top level.
func RenderInitBlock(owner string) string {
	if owner == "" {
		owner = "acme-org"
	}
	return strings.ReplaceAll(initBlockTemplate, "{{OWNER}}", owner)
}
