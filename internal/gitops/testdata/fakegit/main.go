// Command fakegit is a stand-in "git" binary used by tests that need to
// control git subprocess behavior deterministically (sleep past a timeout,
// or fail with a specific stderr message) without touching the network or a
// real repository. It is compiled on demand by the tests that need it (via
// `go build`) and never built as part of the module's normal packages
// (testdata directories are ignored by the go tool).
//
// Behavior is controlled entirely via env vars so the tests that spawn it
// (by prepending its directory to PATH) don't need extra command-line
// plumbing:
//   - FAKEGIT_SLEEP_MS: sleep this many milliseconds before exiting 0.
//   - FAKEGIT_FAIL_STDERR: if set, write this string to stderr and exit 1.
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

func main() {
	if sleepMS := os.Getenv("FAKEGIT_SLEEP_MS"); sleepMS != "" {
		if ms, err := strconv.Atoi(sleepMS); err == nil {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
	}
	if failMsg := os.Getenv("FAKEGIT_FAIL_STDERR"); failMsg != "" {
		fmt.Fprintln(os.Stderr, failMsg)
		os.Exit(1)
	}
	os.Exit(0)
}
