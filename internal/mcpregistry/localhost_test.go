package mcpregistry

import "testing"

// The verdicts are those of the Oracle's _is_local_or_metadata_host under
// Python 3.12 (install/mcp/registry.py:47-63, utils/net.py:9-58), measured
// for each host with the Oracle 0.33.0 venv.
func TestLocalHost_OracleVerdicts(t *testing.T) {
	local := []string{"127.0.0.1", "localhost", "LOCALHOST.", "ip6-localhost", "10.1.2.3", "172.16.0.1",
		"192.168.1.1", "169.254.169.254", "[::1]", "[fe80::1]", "[fc00::1]", "[::ffff:10.0.0.1]",
		"2130706433", "0x7f000001", "0.0.0.0", "[::]", "224.0.0.1", "[ff02::1]", "192.0.0.8",
		"[2001:db8::1]", "198.18.0.1", "240.0.0.1", "255.255.255.255", "[64:ff9b:1::1]", "[2002::1]", "[2001::1]"}
	remote := []string{"172.32.0.1", "8.8.8.8", "[::ffff:8.8.8.8]", "100.64.0.1", "192.0.0.9", "example.com",
		"[2001:4860::1]", "010.0.0.1", "127.1", "[2001:1::1]"}
	for _, h := range local {
		if _, ok := LocalHost("http://" + h + ":8080/v0"); !ok {
			t.Errorf("%s: want a local host", h)
		}
	}
	for _, h := range remote {
		if host, ok := LocalHost("https://" + h + "/v0"); ok {
			t.Errorf("%s: got local host %q, want none", h, host)
		}
	}
	if host, _ := LocalHost("http://LOCALHOST.:1"); host != "localhost." {
		t.Errorf("host = %q, want the lowercased host as urlparse gives it", host)
	}
}
