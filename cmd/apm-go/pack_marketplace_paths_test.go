package main

import (
	"errors"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const packPathsLocalYML = packWarningsRoot + `marketplace:
  owner:
    name: APM Tests
  packages:
    - name: pkg-a
      source: ./pkgs/pkg-a
      category: Productivity
`

const packPathsRemoteYML = packWarningsRoot + `marketplace:
  owner:
    name: APM Tests
  packages:
    - name: remote-tool
      source: acme/remote-tool
      ref: 0123456789abcdef0123456789abcdef01234567
      category: Productivity
`

func TestPack_StrictMetadata_ConfigErrorComesFirst(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, packWarningsRoot+`marketplace:
  owner:
    name: APM Tests
  packages:
    - name: pkg-a
      source: ./one
      category: Productivity
    - name: pkg-a
      source: ./two
      category: Productivity
`)

	out, _, exit := runPackLikeMain(t, []string{"--strict-metadata"})
	if exit != 1 || !strings.Contains(out, "Duplicate package name 'pkg-a'") {
		t.Errorf("exit %d, want 1 with the config error:\n%s", exit, out)
	}
}

func TestPack_Verbose_ListsResolvedPackages(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, packPathsLocalYML)
	writePackFixtureFile(t, "pkgs/pkg-a/apm.yml", "name: pkg-a\ndescription: A local package\nversion: 1.0.0\n")

	out, _, exit := runPackLikeMain(t, []string{"--verbose"})
	if exit != 0 || !strings.Contains(out, "  - pkg-a\n") {
		t.Errorf("exit %d, want the package listed:\n%s", exit, out)
	}
}

func TestPack_UnreadableProjectRoot_FailsBeforeWriting(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the process can enter but not list")
	}
	root := chdirTemp(t)
	writePackApmYML(t, packPathsLocalYML)
	writePackFixtureFile(t, "pkgs/pkg-a/apm.yml", "name: pkg-a\ndescription: A local package\nversion: 1.0.0\n")
	if err := os.Chmod(root, 0o311); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })

	out, _, exit := runPackLikeMain(t, nil)
	if exit != 1 || !strings.Contains(out, `open output root "."`) {
		t.Errorf("exit %d, want 1 with the output-root error:\n%s", exit, out)
	}
}

// With the drift gate's report in the envelope, the Oracle keeps a warning
// only when it is not one of the gate's metadata warnings (v0.33.0
// commands/pack.py:629-631); those stay in drift.outputs[].metadata_warnings.
func TestPackJSON_CheckClean_KeepsNonMetadataWarnings(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, packWarningsRoot)
	writePackFixtureFile(t, "marketplace.yml", packWarningsLegacyYML)
	setPackMetadataOutcome(t, "failed:request timed out")

	stdout, _, exit := runPackLikeMain(t, []string{"--check-clean", "--json"})
	if exit != 4 {
		t.Fatalf("exit %d, want 4\n%s", exit, stdout)
	}
	if got, want := jsonWarnings(t, stdout), []string{wantLegacyWarning}; !reflect.DeepEqual(got, want) {
		t.Errorf("warnings = %q, want %q", got, want)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout is closed") }

func TestPackJSON_StrictMetadata_ReportsAFailedWrite(t *testing.T) {
	chdirTemp(t)
	writePackApmYML(t, packPathsRemoteYML)
	setPackMetadataOutcome(t, "failed:request timed out")

	c := packCmd()
	c.SetOut(failingWriter{})
	c.SetErr(io.Discard)
	c.SetArgs([]string{"--strict-metadata", "--json"})
	c.SilenceErrors = true
	if err := c.Execute(); err == nil || err.Error() != "stdout is closed" {
		t.Errorf("err = %v, want the write error", err)
	}
}

func TestWritePackJSON_UnencodableEnvelopeIsAnError(t *testing.T) {
	var out strings.Builder
	if err := writePackJSON(&out, make(chan int), false); err == nil || out.Len() != 0 {
		t.Errorf("err = %v, wrote %q; want an error and no output", err, out.String())
	}
}

// With -m none the producer resolves nothing, so the drift gate resolves on
// its own.
func TestPack_CheckClean_NoOutputSelected_GateResolves(t *testing.T) {
	t.Run("a resolve error fails the run", func(t *testing.T) {
		chdirTemp(t)
		writePackApmYML(t, packPathsRemoteYML)

		out, _, exit := runPackLikeMain(t, []string{"-m", "none", "--check-clean", "--offline"})
		if exit != 1 || !strings.Contains(out, `package "remote-tool"`) {
			t.Errorf("exit %d, want 1 naming the package:\n%s", exit, out)
		}
	})
	t.Run("a local package's unreadable apm.yml is reported", func(t *testing.T) {
		chdirTemp(t)
		writePackApmYML(t, packPathsLocalYML)
		writePackFixtureFile(t, "pkgs/pkg-a/apm.yml", "name: pkg-a\ndescription: [unclosed\n")

		out, _, _ := runPackLikeMain(t, []string{"-m", "none", "--check-clean"})
		if !strings.Contains(out, ` ! package "pkg-a": could not parse local apm.yml metadata`) {
			t.Errorf("want the local metadata line:\n%s", out)
		}
	})
}
