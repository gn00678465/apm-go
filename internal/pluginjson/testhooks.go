package pluginjson

// commitHook, when non-nil, is consulted before each file's commit rename in
// StagedScaffold.Commit; a returned error aborts the commit at that point.
// Test-only: lets cmd/apm-go prove the rollback path without relying on
// OS-specific permission behaviour.
var commitHook func(name string) error

// SetCommitHookForTest installs hook and returns a restore func.
func SetCommitHookForTest(hook func(name string) error) (restore func()) {
	prev := commitHook
	commitHook = hook
	return func() { commitHook = prev }
}
