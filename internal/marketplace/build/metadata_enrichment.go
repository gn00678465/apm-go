package build

import (
	"fmt"
	"strings"
)

// MetadataStatus is how one resolved package's description/version metadata
// was obtained (Oracle v0.33.0 marketplace/builder.py:77-80).
type MetadataStatus string

const (
	// MetadataFetched: the remote apm.yml supplied a description or a version.
	MetadataFetched MetadataStatus = "fetched"
	// MetadataEmpty: the remote apm.yml was read and supplied neither.
	MetadataEmpty MetadataStatus = "empty"
	// MetadataFailed: the remote apm.yml could not be read.
	MetadataFailed MetadataStatus = "failed"
	// MetadataOffline: the fetch was skipped by --offline.
	MetadataOffline MetadataStatus = "offline"
	// MetadataLocal: a local package, whether or not its apm.yml was readable.
	MetadataLocal MetadataStatus = "local"
	// MetadataExplicit: a remote package whose curator entry fixes both a
	// description and a display version, so nothing is fetched.
	MetadataExplicit MetadataStatus = "explicit"
)

// MetadataOutcome is the metadata result for one resolved package. Cause is
// set only for MetadataFailed and MetadataOffline, and never carries raw
// error text, a URL or a credential.
type MetadataOutcome struct {
	Package string
	Status  MetadataStatus
	Cause   string
}

// Metadata fetch causes. apm-go reads a remote apm.yml with `git clone`
// where the Oracle uses the GitHub HTTP API (builder.py:97-107), so the
// Oracle's `HTTP <code>` and exception-name causes have no counterpart here:
// a failure with the same meaning uses the Oracle's string, and every other
// failure uses metadataCauseUnreadable.
const (
	metadataCauseOffline     = "metadata fetch skipped by --offline"
	metadataCauseTimeout     = "request timed out"
	metadataCauseNetwork     = "network request failed"
	metadataCauseAuth        = "authentication required"
	metadataCauseUnreadable  = "remote apm.yml could not be read"
	metadataCauseUnsupported = "metadata fetch is unsupported for host"
)

// MetadataFetchError is a MetadataFetcher failure that names its cause. A
// fetcher error of any other type is reported as metadataCauseUnreadable.
type MetadataFetchError struct {
	Cause string
	Err   error
}

func (e *MetadataFetchError) Error() string { return e.Err.Error() }
func (e *MetadataFetchError) Unwrap() error { return e.Err }

// MetadataEnrichment is the metadata state of one marketplace build: one
// outcome per resolved package, in resolved order (builder.py:174-243).
type MetadataEnrichment struct {
	Outcomes []MetadataOutcome
}

// Certifiable reports whether output built from this metadata may certify a
// clean regeneration. An unknown status is not certifiable.
func (m MetadataEnrichment) Certifiable() bool {
	for _, o := range m.Outcomes {
		switch o.Status {
		case MetadataFetched, MetadataEmpty, MetadataLocal, MetadataExplicit:
		default:
			return false
		}
	}
	return true
}

// Warnings returns one line per outcome that is not certifiable
// (builder.py:211-235, verbatim).
func (m MetadataEnrichment) Warnings() []string {
	var warnings []string
	for _, o := range m.Outcomes {
		switch o.Status {
		case MetadataOffline:
			warnings = append(warnings, fmt.Sprintf(
				"Package '%s': metadata enrichment skipped by --offline. Rerun without --offline when certification is required.",
				o.Package))
		case MetadataFailed:
			cause := o.Cause
			if cause == "" {
				cause = "unknown failure"
			}
			if strings.HasPrefix(cause, metadataCauseUnsupported) {
				warnings = append(warnings, fmt.Sprintf(
					"Package '%s': metadata enrichment failed (%s). Add fixed description and version fields to certify this package.",
					o.Package, cause))
			} else {
				warnings = append(warnings, fmt.Sprintf(
					"Package '%s': metadata enrichment failed (%s). Restore repository, network, or credential access and retry.",
					o.Package, cause))
			}
		}
	}
	return warnings
}

// UsesRemoteMetadata reports whether an output profile's mapper reads package
// metadata. Only the Claude mapper does (output_mappers.py:93,109).
func UsesRemoteMetadata(format string) bool {
	return format == "claude"
}

// MetadataFor returns the metadata state format's output is composed from,
// or nil when the profile does not use package metadata. resolved must come
// from a ResolvePackages call that did not skip remote metadata.
func MetadataFor(format string, resolved []ResolvedPackage) *MetadataEnrichment {
	if !UsesRemoteMetadata(format) {
		return nil
	}
	m := MetadataEnrichment{Outcomes: make([]MetadataOutcome, 0, len(resolved))}
	for _, pkg := range resolved {
		m.Outcomes = append(m.Outcomes, pkg.Metadata)
	}
	return &m
}
