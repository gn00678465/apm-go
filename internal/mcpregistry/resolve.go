package mcpregistry

import (
	"context"
	"fmt"

	"github.com/apm-go/apm/internal/manifest"
)

// ResolveDeployable resolves name (optionally pinned at version) against
// client into a deployable self-defined-shaped *manifest.MCPDependency
// (Registry=false, Transport/URL filled in from the registry's first
// remote), validated via manifest.ValidateMCP. transportOverride, when
// non-empty, must be one of the remote transports (http/sse/
// streamable-http) and replaces the registry-reported transport.
// requiredHeaders lists header names (no values -- the registry never
// supplies literal values, only requirement descriptors) the server needs;
// callers decide how to surface that as a diagnostic, since the right
// wording differs between `apm install --mcp` (mentions --header) and the
// general `apm install` MCP deploy path (no equivalent flag).
//
// Shared by `apm install --mcp` (cmd/apm-go) and the general `apm install`
// MCP deploy path (internal/deploy), so registry resolution semantics --
// including the credential-safety checks on registry-supplied URLs -- never
// drift between the two entry points.
//
// A registry miss is reported as an error, not a nil dep, so callers can't
// accidentally treat "not found" as "nothing to deploy, no problem".
func ResolveDeployable(ctx context.Context, client *Client, name, version, transportOverride string) (dep *manifest.MCPDependency, requiredHeaders []string, err error) {
	info, ferr := client.FindServerByReference(ctx, name, version)
	if ferr != nil {
		return nil, nil, fmt.Errorf("mcp registry: %w", ferr)
	}
	if info == nil {
		return nil, nil, fmt.Errorf("MCP server %q not found in registry", name)
	}
	if len(info.Remotes) == 0 {
		if info.HasPackages {
			return nil, nil, fmt.Errorf(
				"MCP server %q only provides package-based (stdio) installation, which apm-go does not yet support; declare it manually with a stdio command after --", name)
		}
		return nil, nil, fmt.Errorf("MCP server %q has no deployable remote endpoint", name)
	}

	remote := info.Remotes[0]
	transport := remote.TransportType
	if transport == "" {
		transport = "http"
	}
	if !IsRemoteTransport(transport) {
		return nil, nil, fmt.Errorf("MCP server %q: unsupported remote transport %q", name, transport)
	}
	if transportOverride != "" {
		if !IsRemoteTransport(transportOverride) {
			return nil, nil, fmt.Errorf("transport %q is not valid for a registry-resolved server; only http, sse, or streamable-http apply", transportOverride)
		}
		transport = transportOverride
	}

	// The deployed server's Name (both the target config's server key and
	// the apm.yml identity) must be the caller-given name verbatim, not a
	// shortened slug of the registry's canonical name -- a derived short
	// name is a different identity than what the caller's own
	// conflict/upsert checks just verified, and could silently collide with
	// an unrelated entry.
	dep = &manifest.MCPDependency{Name: name, Registry: false, Transport: transport, URL: remote.URL}

	// Registry-supplied URLs are not trusted input any more than a
	// self-defined url: a compromised or malicious registry entry could
	// return a URL with embedded credentials, which would otherwise be
	// deployed straight into the target's MCP config file.
	if verr := manifest.ValidateMCP(dep); verr != nil {
		return nil, nil, fmt.Errorf("MCP server %q: registry returned an invalid entry: %w", name, verr)
	}

	return dep, remote.RequiredHeaders, nil
}

// IsRemoteTransport reports whether t is a network-reachable MCP transport
// this package can resolve (as opposed to "stdio", which is package-based
// and out of scope for registry resolution).
func IsRemoteTransport(t string) bool {
	return t == "http" || t == "sse" || t == "streamable-http"
}
