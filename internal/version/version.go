// Package version is the single source of truth for apm-go's own release
// version. Bump the const here on release; every other place that needs
// apm-go's version (the root command's --version flag, the lockfile
// apm_version field, the AGENTS.md "APM Version" comment) references it.
package version

// Version is apm-go's release version (SemVer).
const Version = "0.2.1"
