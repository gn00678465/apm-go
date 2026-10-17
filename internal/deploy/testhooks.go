package deploy

// SetSymlinkProbeForTest replaces the check of whether a --global deploy can
// make symlinks and returns a restore func. Test-only: the production check
// fails only on a Windows account without the privilege.
func SetSymlinkProbeForTest(probe func() bool) (restore func()) {
	prev := symlinkProbe
	symlinkProbe = probe
	return func() { symlinkProbe = prev }
}
