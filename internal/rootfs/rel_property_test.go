package rootfs

import (
	"math/rand"
	"path/filepath"
	"testing"
	"testing/quick"
)

// Property: for any clean relative path made of plain components, Rel maps
// root/<p> back to <p>, and root/../<p> is refused as outside the root.
func TestRelContainmentProperty(t *testing.T) {
	dir := t.TempDir()
	rw, err := OpenRootWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	f := func(n uint8, seed [4]byte) bool {
		parts := make([]string, 0, 4)
		for i := 0; i < 1+int(n)%4; i++ {
			parts = append(parts, string([]byte{'a' + seed[i]%26, 'a' + seed[(i+1)%4]%26}))
		}
		p := filepath.Join(parts...)
		got, err := rw.Rel(filepath.Join(dir, p))
		if err != nil || got != p {
			return false
		}
		_, err = rw.Rel(filepath.Join(dir, "..", p))
		return err != nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 300, Rand: rand.New(rand.NewSource(4))}); err != nil {
		t.Fatal(err)
	}
}
