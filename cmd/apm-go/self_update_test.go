package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/version"
)

func TestSelfUpdateCmd_DevBuild(t *testing.T) {
	orig := version.Version
	version.Version = "dev"
	defer func() { version.Version = orig }()

	ux.Init()
	root := buildRootCmd()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"self-update"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for dev build")
	}
	if !strings.Contains(err.Error(), "development builds") {
		t.Errorf("expected 'development builds' in error, got: %s", err)
	}
}

func TestSelfUpdateCmd_HelpText(t *testing.T) {
	ux.Init()
	root := buildRootCmd()

	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetArgs([]string{"self-update", "--help"})

	err := root.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Update the apm-go binary") {
		t.Errorf("expected help text to contain 'Update the apm-go binary', got: %s", out)
	}
	if !strings.Contains(out, "--check") {
		t.Errorf("expected help text to contain '--check', got: %s", out)
	}
}
