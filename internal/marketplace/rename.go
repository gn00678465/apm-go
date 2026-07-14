package marketplace

import (
	"os"
	"time"
)

// renameFn is the injectable rename function used by renameWithRetry, so
// tests can simulate a transient failure without needing a real OS-level
// file lock.
var renameFn = os.Rename

// renameWithRetry commits oldpath over newpath via os.Rename, retrying a few
// times on failure. On Windows a transient lock on newpath (AV scan, a
// concurrent apm-go run) can make a single os.Rename fail with
// ACCESS_DENIED/SHARING_VIOLATION even though the rename would succeed
// moments later.
// ponytail: fixed small retry; per-OS tuning if flakiness shows
func renameWithRetry(oldpath, newpath string) error {
	const attempts = 3
	const backoff = 60 * time.Millisecond
	var err error
	for i := 0; i < attempts; i++ {
		if err = renameFn(oldpath, newpath); err == nil {
			return nil
		}
		if i < attempts-1 {
			time.Sleep(backoff)
		}
	}
	return err
}
