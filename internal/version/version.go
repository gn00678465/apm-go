// Package version is the single source of truth for apm-go's own release
// version. Every place that needs it (the root command's --version flag, the
// lockfile apm_version field, the AGENTS.md "APM Version" comment) references
// Version.
package version

// Version is apm-go's release version (SemVer). Release builds inject it from
// the git tag at link time:
//
//	go build -ldflags "-X github.com/apm-go/apm/internal/version.Version=<tag>"
//
// (release.yml does this with the pushed tag, stripped of its "v" prefix.)
// Local builds keep the fallback below, so a dev binary reports "dev".
var Version = "dev"
