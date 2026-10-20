package manifest

import (
	"errors"
	"fmt"
	"strings"
)

// IsInsecure reports whether dep is fetched over plain http (the Oracle's
// is_insecure).
func (d *DependencyReference) IsInsecure() bool {
	return d != nil && d.Scheme == "http"
}

// CheckInsecureDependency is the Oracle's _check_insecure_dependencies for
// one direct dependency (install/insecure_policy.py:229-264): an http://
// dependency needs both allow_insecure: true in apm.yml and --allow-insecure,
// and the error names the steps that are missing.
func CheckInsecureDependency(dep *DependencyReference, allowFlag bool, defaultHost string) error {
	if !dep.IsInsecure() || (dep.AllowInsecure && allowFlag) {
		return nil
	}
	return errors.New(InsecureDependencyRequirements(InsecureDependencyURL(dep, defaultHost), !dep.AllowInsecure, !allowFlag))
}

// InsecureDependencyRequirements is _format_insecure_dependency_requirements
// (install/insecure_policy.py:62-91); the step names apm-go.
func InsecureDependencyRequirements(url string, missingDepAllow, missingFlag bool) string {
	lines := []string{url + " -- HTTP dependency (unencrypted)", "To install:"}
	step := 1
	if missingDepAllow {
		lines = append(lines, fmt.Sprintf("  %d. Set allow_insecure: true on the dep in apm.yml", step))
		step++
	}
	if missingFlag {
		lines = append(lines, fmt.Sprintf("  %d. Pass --allow-insecure to apm-go install", step))
	}
	return strings.Join(lines, "\n")
}

// InsecureDependencyURL is _get_insecure_dependency_url
// (install/insecure_policy.py:51-59): the `git` value to_apm_yml_entry
// writes for an http dependency, http://<host>[:<port>]/<repo_url>
// (models/dependency/reference.py:1956-1961), and #<ref> when it has one.
func InsecureDependencyURL(dep *DependencyReference, defaultHost string) string {
	host := dep.Host
	if host == "" {
		host = defaultHost
	}
	if host == "" {
		host = "github.com"
	}
	if dep.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, dep.Port)
	}
	url := "http://" + host + "/" + dep.RepoURL
	if dep.Reference != "" {
		url += "#" + dep.Reference
	}
	return url
}
