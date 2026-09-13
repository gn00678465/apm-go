//go:build windows

package main

// rootOpenExtraFlags is ORed into os.O_RDONLY for every manifest open that goes
// through (*os.Root).OpenFile. Windows has no O_NONBLOCK and no FIFO type, so
// there is nothing to add; the constant exists so both platforms feed the same
// call site (openRootFile in plugin_validate.go).
//
// Win32 can open a link without following it (FILE_FLAG_OPEN_REPARSE_POINT),
// but Go exposes no way to pass it through (*os.Root).OpenFile, so this open
// still follows a symlink whose target stays inside the boundary. os.Root
// refuses a target outside the boundary, and the Fstat comparison after the
// open catches a substituted file; a link resolving to the very file the
// pre-open Lstat saw is indistinguishable to that comparison and is accepted.
const rootOpenExtraFlags = 0
