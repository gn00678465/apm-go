package main

import (
	"os"
	"testing"
	"time"

	"charm.land/huh/v2"

	"github.com/apm-go/apm/internal/ux"
)

// TestMarketplaceInitCmd_NoInteractivePrompt_Behavioral is AC53/D13's
// behavioral backstop to TestMarketplaceInitCmd_NoInteractiveComponents
// (ac53_interactive_gate_test.go), which proves non-interactivity by
// statically walking marketplaceInitCmd's call graph via go/ast. That
// static analysis is (honestly, by its own doc comment) unsound against a
// handful of indirection styles it cannot resolve without go/types:
//
//   - a call reached through a struct field, slice element, or map value of
//     function type (e.g. `h.prompt(...)`, `fs[0](...)`, `fm["p"](...)`) --
//     ac53FindViolations only recognizes a *ast.CallExpr whose Fun is a bare
//     *ast.Ident (a directly-named function/var) or a *ast.SelectorExpr
//     matching a known huh/ux import binding; a SelectorExpr or IndexExpr
//     reaching an arbitrary field/element of function type falls through
//     both cases silently.
//   - a method declared on a GENERIC receiver (e.g. `func (p prompt[T])
//     Run()`) -- ac53ReceiverTypeName only unwraps a plain (possibly
//     pointer) *ast.Ident receiver type expression; a generic receiver's
//     type expression is an *ast.IndexExpr (or *ast.IndexListExpr for more
//     than one type parameter), so the method is never registered in
//     callables at all and no call graph edge can ever reach it.
//
// Both are genuine, demonstrated gaps in the AST gate (see the "mutation
// test" note below), not just theoretical ones, and both are also
// explicitly out of scope for that gate to close without adding go/types
// (a much larger surface than go/ast+go/parser alone) -- see
// ac53_interactive_gate_test.go's own doc comment.
//
// This test closes the gap a different way: instead of asking "does the
// source text contain a call that COULD prompt", it asks "did running the
// command actually reach a prompt". Every one of ux.Confirm, ux.MultiSelect,
// ux.InputForm, and Clack's Confirm/Form/MultiSelect wrappers funnels
// through exactly one of three package-level seams (confirmWith/
// multiSelectWith/inputFormWith, internal/ux/interactive.go and clack.go) --
// regardless of how deeply indirected, generically dispatched, or
// dynamically constructed the call site that reaches them is. Stubbing
// those seams via ux.SetPromptSeamsForTest and forcing CanPrompt() to true
// via ux.SetTTYSeamsForTest (so a real interactive branch, if one existed,
// would not silently no-op on "prompting isn't possible") turns "was a
// prompt seam invoked" into a runtime fact this test can observe directly,
// with no reliance on being able to statically trace the call site that
// invoked it.
//
// Residual scope limit, honestly disclosed (matching this whole task's
// convention): a hypothetical implementation that prompted via some
// entirely different mechanism -- reading os.Stdin directly, or calling
// huh's own API without going through internal/ux's seams at all -- would
// not be caught here either. The former has no legitimate reason to exist
// in this command (marketplaceInitCmd takes no arguments needing
// confirmation) and the latter is exactly what
// TestMarketplaceInitCmd_NoInteractiveComponents's blanket ban on any bound
// huh identifier already exists to catch (huh's entire exported surface is
// interactive; there is no non-interactive reason to reference it at all).
//
// Mutation test performed while writing this test (not left in the tree):
// a struct-field-typed call (`h := holder{prompt: ux.Confirm}; h.prompt(...)`)
// was tried first and did NOT demonstrate a real gap -- the AST gate's
// generic *ast.SelectorExpr visitor still flags the literal `ux.Confirm`
// selector appearing as the composite literal's field value, independent of
// whether it is ever called. The genuine gap is the generic-receiver one:
// added, directly inside marketplaceInitCmd's RunE, a call to a
// package-level var of a generic-receiver type,
//
//	type ac53MutationPrompt[T any] struct{}
//	func (ac53MutationPrompt[T]) Run() { _, _ = ux.Confirm("continue?", false) }
//	var ac53MutationRunner ac53MutationPrompt[int]
//	// RunE: ac53MutationRunner.Run()
//
// Because ac53ReceiverTypeName only unwraps a plain (possibly pointer)
// *ast.Ident receiver type, `func (ac53MutationPrompt[T]) Run()`'s receiver
// type expression (*ast.IndexExpr) makes ac53ReceiverTypeName return "", so
// resolveAC53Callables never registers this method under any callables key
// at all -- its body (the one containing the literal `ux.Confirm` call) is
// therefore never scanned by ast.Inspect, no matter how it is reached.
// TestMarketplaceInitCmd_NoInteractiveComponents (the AST gate) stayed
// green (PASS) against this mutation, while this test went red
// (confirmCalls == 1), demonstrating it independently catches what the AST
// gate provably cannot. The mutation was then reverted (`git diff` against
// marketplace_authoring.go shows no changes).
func TestMarketplaceInitCmd_NoInteractivePrompt_Behavioral(t *testing.T) {
	chdirTemp(t)

	restoreTTY := ux.SetTTYSeamsForTest(true, true, true)
	t.Cleanup(restoreTTY)

	var confirmCalls, multiSelectCalls, formCalls int
	restorePrompt := ux.SetPromptSeamsForTest(
		func(theme huh.Theme, prompt string, def bool) (bool, error) {
			confirmCalls++
			return def, nil
		},
		func(theme huh.Theme, title, description string, showHelp bool, opts []ux.Option) ([]string, error) {
			multiSelectCalls++
			return nil, nil
		},
		func(theme huh.Theme, title string, showHelp bool, fields []ux.Field) (map[string]string, error) {
			formCalls++
			return nil, nil
		},
	)
	t.Cleanup(restorePrompt)

	cmd := marketplaceInitCmd()
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("marketplace init returned an error with CanPrompt() forced true: %v", err)
	}

	if confirmCalls != 0 || multiSelectCalls != 0 || formCalls != 0 {
		t.Fatalf("marketplace init reached an interactive prompt seam even with CanPrompt() forced true (confirm=%d multiSelect=%d form=%d); AC53/D13 requires marketplace init to stay non-interactive", confirmCalls, multiSelectCalls, formCalls)
	}
}

// TestMarketplaceInitCmd_ForceFlag_NoInteractivePrompt_Behavioral closes a gap
// the test above cannot see (B-BLOCKING-1, external audit,
// marketplace-add-fixes, 2026-07-31): that test always calls
// `cmd.SetArgs(nil)`, so it never actually executes with --force -- the one
// flag that takes marketplaceInitCmd's RunE down the
// spliceMarketplaceBlock(..., force=true) overwrite branch (an existing
// 'marketplace:' block being replaced) instead of the plain-append path. A
// generic-receiver-typed helper reading raw os.Stdin only under --force,
// e.g.
//
//	type forceReader[T any] struct{}
//	func (forceReader[T]) Run() { var a string; _, _ = fmt.Fscan(os.Stdin, &a) }
//	var marketplaceForceReader forceReader[int]
//	// RunE: if force { marketplaceForceReader.Run() }
//
// would bypass every prompt seam stubbed above (it never calls
// confirmWith/multiSelectWith/inputFormWith) AND the AST gate
// (ac53_interactive_gate_test.go's own doc comment already demonstrates a
// generic-receiver method is invisible to it, for the same reason cited in
// this file's own doc comment above), and would never even be exercised by
// the test above since it never passes --force.
//
// This test closes that gap directly: it runs marketplaceInitCmd with
// --force against an apm.yml that already has a non-null 'marketplace:'
// block (so the overwrite branch actually executes), with os.Stdin
// redirected to the read end of a pipe that nothing is ever written to and
// that is not closed until the command has already returned. A production
// implementation that behaves correctly never touches os.Stdin, so the
// command returns immediately; a mutation like the one above would instead
// block forever trying to read from a stream with nothing to give it and no
// EOF to observe -- caught here by the same goroutine+timeout pattern this
// package already uses in plugin_init_interactive_test.go's
// driveInteractiveInit for the same "did it block on an unstubbed read"
// question.
func TestMarketplaceInitCmd_ForceFlag_NoInteractivePrompt_Behavioral(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte(apmYMLWithExistingBlock), 0o644); err != nil {
		t.Fatal(err)
	}

	restoreTTY := ux.SetTTYSeamsForTest(true, true, true)
	t.Cleanup(restoreTTY)

	var confirmCalls, multiSelectCalls, formCalls int
	restorePrompt := ux.SetPromptSeamsForTest(
		func(theme huh.Theme, prompt string, def bool) (bool, error) {
			confirmCalls++
			return def, nil
		},
		func(theme huh.Theme, title, description string, showHelp bool, opts []ux.Option) ([]string, error) {
			multiSelectCalls++
			return nil, nil
		},
		func(theme huh.Theme, title string, showHelp bool, fields []ux.Field) (map[string]string, error) {
			formCalls++
			return nil, nil
		},
	)
	t.Cleanup(restorePrompt)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() err = %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		r.Close()
		w.Close()
	})

	cmd := marketplaceInitCmd()
	cmd.SetArgs([]string{"--force", "--owner", "new-org"})

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("marketplace init --force returned an error: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("marketplace init --force did not return within timeout; it likely blocked reading os.Stdin (a stdin-reading mutation bypassing every prompt seam and the AST gate, B-BLOCKING-1)")
	}

	if confirmCalls != 0 || multiSelectCalls != 0 || formCalls != 0 {
		t.Fatalf("marketplace init --force reached an interactive prompt seam (confirm=%d multiSelect=%d form=%d); AC53/D13 requires it to stay non-interactive", confirmCalls, multiSelectCalls, formCalls)
	}
}

// TestMarketplaceInitCmd_DoesNotReadStdin closes the escape hatch that the
// other two AC53 gates share: both are name-based. The AST gate
// (TestMarketplaceInitCmd_NoInteractiveComponents) walks the call graph for
// identifiers bound to charm.land/huh or internal/ux; the seam-counting gate
// above only counts calls that go THROUGH ux's prompt seams. A direct read of
// os.Stdin -- `fmt.Fscan(os.Stdin, &x)`, bufio.NewReader(os.Stdin).ReadString,
// os.Stdin.Read -- is neither, so both stay green.
//
// Reproduced before writing this test: inserting
//
//	if !force {
//	    var probe string
//	    fmt.Fscan(os.Stdin, &probe)
//	}
//
// into marketplaceInitCmd left all three existing gates GREEN, while the built
// binary blocked indefinitely on a live stdin (rc=124 under `sleep 60 | timeout
// 8 apm-go marketplace init`; rc=0 with stdin=/dev/null). Tests miss it
// precisely because `go test` hands the process a stdin that EOFs immediately.
//
// This gate is behavioural rather than name-based on purpose: it fails for ANY
// blocking stdin read, including ones nobody has thought to blacklist.
func TestMarketplaceInitCmd_DoesNotReadStdin(t *testing.T) {
	chdirTemp(t)

	restoreTTY := ux.SetTTYSeamsForTest(true, true, true)
	t.Cleanup(restoreTTY)

	// A pipe whose write end is deliberately held open and never written to:
	// any read blocks instead of returning EOF, exactly like an idle terminal.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = w.Close()
		_ = r.Close()
	})

	done := make(chan error, 1)
	go func() {
		cmd := marketplaceInitCmd()
		cmd.SetArgs(nil)
		done <- cmd.Execute()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("marketplace init returned an error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("marketplace init blocked on stdin: it must never read stdin (AC53/D13). " +
			"A name-based gate cannot see this -- os.Stdin reads bypass both the huh/ux AST " +
			"walk and the ux prompt-seam counters.")
	}
}
