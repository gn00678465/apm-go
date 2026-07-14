package marketplace

import (
	"fmt"
	"testing"
)

func TestRenameWithRetry_SucceedsAfterTransientFailure(t *testing.T) {
	orig := renameFn
	t.Cleanup(func() { renameFn = orig })

	calls := 0
	renameFn = func(oldpath, newpath string) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("ACCESS_DENIED")
		}
		return nil
	}

	if err := renameWithRetry("old", "new"); err != nil {
		t.Fatalf("renameWithRetry() error = %v, want nil", err)
	}
	if calls != 2 {
		t.Fatalf("renameFn called %d times, want 2", calls)
	}
}

func TestRenameWithRetry_ReturnsLastErrorWhenAlwaysFails(t *testing.T) {
	orig := renameFn
	t.Cleanup(func() { renameFn = orig })

	calls := 0
	renameFn = func(oldpath, newpath string) error {
		calls++
		return fmt.Errorf("SHARING_VIOLATION attempt %d", calls)
	}

	err := renameWithRetry("old", "new")
	if err == nil {
		t.Fatal("renameWithRetry() error = nil, want non-nil")
	}
	if got, want := err.Error(), "SHARING_VIOLATION attempt 3"; got != want {
		t.Fatalf("renameWithRetry() error = %q, want %q", got, want)
	}
	if calls != 3 {
		t.Fatalf("renameFn called %d times, want 3", calls)
	}
}
