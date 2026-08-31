package gitops

import (
	"fmt"
	"strings"
)

// GitErrorKind classifies a git failure, mirroring upstream's GitErrorKind
// (marketplace/git_stderr.py:33-41).
type GitErrorKind string

const (
	GitErrorAuth     GitErrorKind = "auth"
	GitErrorNotFound GitErrorKind = "not_found"
	GitErrorTimeout  GitErrorKind = "timeout"
	GitErrorUnknown  GitErrorKind = "unknown"
)

// TranslatedGitError is the structured result of TranslateGitStderr
// (git_stderr.py:44-51).
type TranslatedGitError struct {
	Kind    GitErrorKind
	Summary string
	Hint    string
	Raw     string
}

// Pattern tables, lower-cased, priority order (git_stderr.py:56-92).
var (
	gitAuthPatterns = []string{
		"authentication failed",
		"invalid credentials",
		"could not read password",
		"permission denied (publickey)",
		"403 forbidden",
		"401 unauthorized",
		"fatal: authentication",
		"remote: write access",
		"please make sure you have the correct access rights",
		"the requested url returned error: 401",
		"the requested url returned error: 403",
	}
	gitNotFoundPatterns = []string{
		"repository not found",
		"does not appear to be a git repository",
		"not a valid ref",
		"couldn't find remote ref",
		"could not resolve",
		"the requested url returned error: 404",
		"no such ref",
		"unknown ref",
	}
	gitTimeoutPatterns = []string{
		"operation timed out",
		"connection timed out",
		"could not resolve host",
		"connection refused",
		"network is unreachable",
		"temporary failure in name resolution",
		"ssl_read: connection reset",
		"early eof",
		"rpc failed",
	}
)

const gitRawMaxLen = 500

// TranslateGitStderr classifies git stderr text into a known failure mode
// and produces an actionable hint (git_stderr.py:157-171).
func TranslateGitStderr(stderr string, exitCode int, operation, remote string) TranslatedGitError {
	kind := classifyGitStderr(strings.ToLower(stderr))
	return TranslatedGitError{
		Kind:    kind,
		Summary: gitErrorSummary(kind, operation, exitCode),
		Hint:    gitErrorHint(kind, operation, remote),
		Raw:     truncateRaw(stderr),
	}
}

// classifyGitStderr mirrors _classify (git_stderr.py:100-128): auth first,
// then not-found, then timeout; "could not resolve host" is DNS (timeout),
// not not-found, despite matching the shorter "could not resolve".
func classifyGitStderr(lower string) GitErrorKind {
	for _, p := range gitAuthPatterns {
		if strings.Contains(lower, p) {
			return GitErrorAuth
		}
	}
	for _, p := range gitNotFoundPatterns {
		if p == "could not resolve" && strings.Contains(lower, "could not resolve host") {
			continue
		}
		if strings.Contains(lower, p) {
			return GitErrorNotFound
		}
	}
	for _, p := range gitTimeoutPatterns {
		if strings.Contains(lower, p) {
			return GitErrorTimeout
		}
	}
	return GitErrorUnknown
}

const gitSummaryMaxLen = 80

// gitErrorSummary mirrors _build_summary (git_stderr.py:125-141). exitCode
// < 0 means "unknown" (upstream's None).
func gitErrorSummary(kind GitErrorKind, operation string, exitCode int) string {
	var text string
	switch {
	case kind == GitErrorAuth:
		text = fmt.Sprintf("Git authentication failed during %s.", operation)
	case kind == GitErrorNotFound:
		text = fmt.Sprintf("Git ref or repository not found during %s.", operation)
	case kind == GitErrorTimeout:
		text = fmt.Sprintf("Git network timeout during %s.", operation)
	case exitCode >= 0:
		text = fmt.Sprintf("Git failed during %s (exit %d).", operation, exitCode)
	default:
		text = fmt.Sprintf("Git failed during %s.", operation)
	}
	if len(text) > gitSummaryMaxLen {
		text = text[:gitSummaryMaxLen-3] + "..."
	}
	return text
}

// gitErrorHint mirrors _build_hint (git_stderr.py:144-154).
func gitErrorHint(kind GitErrorKind, operation, remote string) string {
	switch kind {
	case GitErrorAuth:
		return "Check your GITHUB_TOKEN / gh auth / SSH key. Run 'apm-go doctor' to diagnose."
	case GitErrorNotFound:
		label := "the remote"
		if remote != "" {
			label = "'" + remote + "'"
		}
		return fmt.Sprintf("Verify the remote %s exists and the ref is spelled correctly.", label)
	case GitErrorTimeout:
		return "Network issue contacting the remote. Retry or check your connection."
	}
	return fmt.Sprintf("Git failed during %s. See raw stderr above.", operation)
}

func truncateRaw(s string) string {
	if len(s) <= gitRawMaxLen {
		return s
	}
	return s[:gitRawMaxLen] + "... (truncated)"
}
