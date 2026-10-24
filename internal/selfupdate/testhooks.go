package selfupdate

// SetReleasesForTest points the release lookup at apiBase and makes Apply
// replace binary in place of the running executable, so a test in another
// package can run self-update against an httptest server. The returned func
// restores both.
func SetReleasesForTest(apiBase, binary string) (restore func()) {
	prevBase, prevExec := apiBaseURL, osExecutable
	apiBaseURL = apiBase
	osExecutable = func() (string, error) { return binary, nil }
	return func() { apiBaseURL, osExecutable = prevBase, prevExec }
}
