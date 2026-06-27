package yamlcore

// IsVendorExtKey reports whether key matches the vendor extension pattern
// x-[a-z][a-z0-9-]* defined in OpenAPM v0.1 §4.1 (req-ext-001).
func IsVendorExtKey(key string) bool {
	if len(key) < 3 || key[0] != 'x' || key[1] != '-' {
		return false
	}
	c := key[2]
	if c < 'a' || c > 'z' {
		return false
	}
	for i := 3; i < len(key); i++ {
		c = key[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}
