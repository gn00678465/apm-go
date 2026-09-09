// Crash-oracle fuzz target for NFR-002 (no panic, no memory exhaustion on
// arbitrary input; the 5 MiB cap rejects oversize input before content is
// read). This is deliberately crash-oracle-only -- it asserts nothing about
// Validate's returned Report beyond "it returns" -- correctness is
// validate_test.go's (fixed scenarios) and validate_property_test.go's
// (randomized known-field witness) job.
package pluginjson

import (
	"bytes"
	"testing"
)

// FuzzValidateBytes calls Validate([]byte) directly (not through the CLI).
// Go's native fuzzer catches panics and hangs on its own; no explicit
// assertion is needed inside f.Fuzz's callback.
func FuzzValidateBytes(f *testing.F) {
	// One seed per spec.md User Story 3 "Structure" scenario.
	f.Add([]byte(`{`))                                                                                                    // AS1: unterminated JSON.
	f.Add([]byte(`[]`))                                                                                                   // AS2: valid JSON, wrong top-level shape.
	f.Add([]byte(``))                                                                                                     // empty byte slice.
	f.Add([]byte{0xFF})                                                                                                   // invalid UTF-8.
	f.Add(bytes.Repeat([]byte("["), 1024*1024))                                                                           // AS3-adjacent: deeply nested (1 MiB marker).
	f.Add(bytes.Repeat([]byte("a"), 6*1024*1024))                                                                         // AS3: oversize (>5 MiB cap), representative marker.
	f.Add([]byte(`{"name":"a","name":"b"}`))                                                                              // AS4: duplicate top-level key.
	f.Add([]byte(`{"name":"demo-plugin","version":"1.0.0","description":"demo plugin","author":{"name":"Demo Author"}}`)) // one well-formed, zero-finding manifest.

	f.Fuzz(func(t *testing.T, data []byte) {
		Validate(data)
	})
}
