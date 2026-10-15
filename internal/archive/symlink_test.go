package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// symlinkFixture makes a package root, symlinks resolved, with files and
// symlinks below it (path -> link text, "<outside>" and "<root>" replaced),
// and a directory outside it.
func symlinkFixture(t *testing.T, files, links map[string]string) (root, outside string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, outside = filepath.Join(base, "pkg"), filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(outside, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, target := range links {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(placeholders(target, root, outside), p); err != nil {
			t.Skipf("symlink unsupported on this platform: %v", err)
		}
	}
	return root, outside
}

func TestResolveIn(t *testing.T) {
	for _, tt := range []struct {
		name    string
		links   map[string]string
		rel     string
		want    string
		wantErr string
	}{
		{name: "no symlink", rel: "a/b.md", want: "a/b.md"},
		{name: "missing component is left to the reader", rel: "x/y.md", want: "x/y.md"},
		{name: "file symlink in the package", links: map[string]string{"a/l.md": "b.md"}, rel: "a/l.md", want: "a/b.md"},
		{name: "directory symlink in the package", links: map[string]string{"d": "a"}, rel: "d/b.md", want: "a/b.md"},
		{name: "file symlink outside", links: map[string]string{"a/l.md": "<outside>/secret"}, rel: "a/l.md",
			wantErr: "symlink a/l.md resolves to <outside>/secret, outside the package"},
		{name: "directory symlink outside", links: map[string]string{"d": "<outside>/dir"}, rel: "d/b.md",
			wantErr: "symlink d resolves to <outside>/dir, outside the package"},
		{name: "relative symlink out through ..", links: map[string]string{"a/l.md": "../../outside/secret"}, rel: "a/l.md",
			wantErr: "symlink a/l.md resolves to <outside>/secret, outside the package"},
		{name: "dangling", links: map[string]string{"a/l.md": "gone.md"}, rel: "a/l.md",
			wantErr: "symlink a/l.md -> gone.md does not resolve: no such file or directory"},
		{name: "loop", links: map[string]string{"a/l.md": "m.md", "a/m.md": "l.md"}, rel: "a/l.md",
			wantErr: "symlink a/l.md is a symlink loop"},
		{name: "parent component", rel: "../outside/secret", wantErr: "path ../outside/secret leaves the package"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, outside := symlinkFixture(t, map[string]string{"a/b.md": "b"}, tt.links)
			got, err := ResolveIn(root, tt.rel)
			if tt.wantErr != "" {
				if want := placeholders(tt.wantErr, root, outside); err == nil || err.Error() != want {
					t.Fatalf("ResolveIn(%q) = %q, %v; want error %q", tt.rel, got, err, want)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("ResolveIn(%q) = %q, %v; want %q", tt.rel, got, err, tt.want)
			}
		})
	}
}

func placeholders(s, root, outside string) string {
	return strings.NewReplacer("<root>", root, "<outside>", outside).Replace(s)
}

func TestCheckTree(t *testing.T) {
	for _, tt := range []struct {
		name    string
		links   map[string]string
		wantErr string
	}{
		{name: "regular files"},
		{name: "symlinks in the package", links: map[string]string{"s/l.md": "../other/o.md", "s/d": "../other"}},
		{name: "file symlink outside, after the first in name order",
			links:   map[string]string{"s/b": "<outside>/secret", "s/z": "<outside>/secret"},
			wantErr: "symlink s/b resolves to <outside>/secret, outside the package"},
		{name: "symlink outside below a directory symlink",
			links:   map[string]string{"s/d": "../other", "other/x": "<outside>/secret"},
			wantErr: "symlink s/d/x resolves to <outside>/secret, outside the package"},
		{name: "symlink to the directory it is in",
			links:   map[string]string{"s/self": "."},
			wantErr: "symlink s/self resolves to <root>/s, a directory it is in (symlink loop)"},
		{name: "symlink to a parent", links: map[string]string{"s/up": ".."},
			wantErr: "symlink s/up resolves to <root>, a directory it is in (symlink loop)"},
		{name: "two directory symlinks into each other",
			links:   map[string]string{"s/d": "../other", "other/back": "../s/d"},
			wantErr: "symlink s/d/back resolves to <root>/other, a directory it is in (symlink loop)"},
		{name: "dangling", links: map[string]string{"s/gone": "nowhere"},
			wantErr: "symlink s/gone -> nowhere does not resolve: no such file or directory"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, outside := symlinkFixture(t, map[string]string{"s/SKILL.md": "s", "other/o.md": "o"}, tt.links)
			err := CheckTree(root, "s")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckTree: %v", err)
				}
				return
			}
			if want := placeholders(tt.wantErr, root, outside); err == nil || err.Error() != want {
				t.Fatalf("CheckTree: %v\nwant %s", err, want)
			}
		})
	}
}
