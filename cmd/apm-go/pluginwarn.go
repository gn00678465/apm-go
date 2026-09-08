package main

import (
	"os"
	"path/filepath"
)

// pluginNativeDirs are the plugin-authoring directories `apm-go pack`
// bundles directly into a plugin export (R4.2), mirroring upstream's
// bundle/plugin_layout.py:5-12.
var pluginNativeDirs = []string{"agents", "skills", "commands", "instructions", "extensions", "hooks"}

// detectPluginNativeRoot reports which of pluginNativeDirs (as a real,
// non-symlink directory) or hooks.json exist directly under root, but only
// when root has no .apm/ (R4.2: "且根目錄沒有 .apm/" short-circuits the
// whole check). Both `apm-go init` and `apm-go plugin init` call this
// against the directory the command was invoked from (runInitCore's
// originalCwd) -- shared body, R4.1.
//
// A-BLOCKING-1 (external audit round 6, 2026-07-31): the PRD's own R4.2
// wording -- "存在...任一目錄（非 symlink）或 hooks.json 檔案" -- places
// "(非 symlink)" immediately after "目錄", modifying only the pluginNativeDirs
// directory check (mirrored below via os.Lstat + an explicit ModeSymlink
// exclusion, matching upstream's plugin_layout.py list and AC16's symlinked-
// skills/-must-not-trigger requirement). Neither ".apm/" nor "hooks.json" is
// covered by that parenthetical, and upstream's own check (Python pathlib's
// `.is_dir()`/`.is_file()`) follows a symlink by default -- so both of the
// following must use os.Stat (which follows a symlink to see what it
// resolves to), NOT os.Lstat (which reports the symlink's own type and
// therefore never recognizes a symlinked ".apm" as a directory, nor a
// symlinked "hooks.json" as a regular file):
//   - ".apm": a project whose ".apm" is a symlink to a real directory
//     elsewhere previously had this check silently miss it (Lstat().IsDir()
//     is false for any symlink, however it resolves) and print the warning
//     even though ".apm/" conceptually exists -- a false positive.
//   - "hooks.json": a project whose "hooks.json" is a symlink to a real file
//     previously had this check's OWN added ModeSymlink exclusion silently
//     skip it, under-reporting a plugin-native source upstream's is_file()
//     would have counted -- an unintended over-restriction with no upstream
//     or PRD basis (unlike the directory list's exclusion, which upstream's
//     own SCP-style `agents/`-must-not-be-symlink rule and AC16 both require).
func detectPluginNativeRoot(root string) []string {
	if fi, err := os.Stat(filepath.Join(root, ".apm")); err == nil && fi.IsDir() {
		return nil
	}

	var found []string
	for _, d := range pluginNativeDirs {
		fi, err := os.Lstat(filepath.Join(root, d))
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			continue
		}
		found = append(found, d+"/")
	}
	if fi, err := os.Stat(filepath.Join(root, "hooks.json")); err == nil && fi.Mode().IsRegular() {
		found = append(found, "hooks.json")
	}
	return found
}
