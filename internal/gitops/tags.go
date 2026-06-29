package gitops

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/apm-go/apm/internal/semver"
)

// RealTagLister implements resolver.TagLister via git ls-remote.
type RealTagLister struct {
	DefaultHost string // e.g. "github.com"
}

func (r *RealTagLister) ListTags(repoURL string) ([]semver.TagInfo, error) {
	cloneURL := r.resolveCloneURL(repoURL)

	cmd := exec.Command("git", "ls-remote", "--tags", "--refs", cloneURL)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git ls-remote %s: %s", cloneURL, string(ee.Stderr))
		}
		return nil, fmt.Errorf("git ls-remote %s: %w", cloneURL, err)
	}

	return parseTagsOutput(string(out)), nil
}

func (r *RealTagLister) resolveCloneURL(repoURL string) string {
	if strings.Contains(repoURL, "://") || strings.HasPrefix(repoURL, "git@") {
		return repoURL
	}
	host := r.DefaultHost
	if host == "" {
		host = "github.com"
	}
	return "https://" + host + "/" + repoURL + ".git"
}

func parseTagsOutput(output string) []semver.TagInfo {
	var tags []semver.TagInfo
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		sha := parts[0]
		ref := parts[1]
		tag := strings.TrimPrefix(ref, "refs/tags/")
		tags = append(tags, semver.TagInfo{Name: tag, Commit: sha})
	}
	return tags
}
