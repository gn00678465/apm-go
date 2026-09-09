//go:build !windows

package main

import "syscall"

// rootOpenExtraFlags is ORed into os.O_RDONLY for every manifest open that
// goes through (*os.Root).OpenFile (WP03 round-4 review findings 1-2, both
// the directory-probed candidate and the caller's own path argument): the
// os.Root boundary itself is what keeps the open from escaping the probed
// directory or the argument's parent directory, but it gives no protection
// against a candidate swapped to a FIFO between the pre-open Lstat and this
// Open -- a plain O_RDONLY blocks forever reading a FIFO with no writer
// attached. O_NONBLOCK is what makes that open return immediately instead,
// on whichever of the two callers reaches it.
const rootOpenExtraFlags = syscall.O_NONBLOCK
