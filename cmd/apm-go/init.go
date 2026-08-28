package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/pluginjson"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/cobra"
)

// initMode captures the behavioral deltas between `apm-go init` (consumer
// mode) and `apm-go plugin init` (plugin mode, cmd/apm-go/plugin.go): both
// commands share the same runInitCore body below (design.md §3), differing
// only in these fields.
type initMode struct {
	// plugin toggles the plugin-specific behavior: devDependencies in
	// apm.yml (manifestSpec.Plugin, R3.3.c), writing plugin.json (R3.3.d),
	// and validateName's kebab-case rule (R3.3.a).
	plugin bool
	// pluginFormat is upstream's plugin_mode (commands/init.py:139): "" for
	// consumer init, pluginModeClaude for the legacy Claude layout,
	// pluginModeAgent for Agent Plugins v1 (which additionally scaffolds
	// mcp.json, commands/init.py:199-202).
	pluginFormat string
	// defaultYesVer is the version written when --yes/non-interactive picks
	// a default (R3.3.b). The interactive form's default is always "1.0.0"
	// for both modes, independent of this field.
	defaultYesVer string
	// validateName validates the resolved project name: the explicit
	// PROJECT-NAME arg, or (plugin mode only, R3.2/AC31) the current
	// directory's basename when the arg is omitted.
	validateName func(string) error
	introTitle   string
	successTitle string
	// nextSteps is rendered verbatim in the shared success block for both
	// consumer and plugin initialization (Oracle commands/init.py:316-370).
	nextSteps []string
}

var consumerMode = initMode{
	plugin:        false,
	defaultYesVer: "1.0.0",
	validateName:  consumerValidateName,
	introTitle:    "Setting up your APM project",
	successTitle:  "APM project initialized successfully!",
	nextSteps: []string{
		"Install a package:               apm-go install <owner>/<repo>",
		"Run a script:                    apm-go run <script>",
		"Build a plugin? Scaffold one:    apm-go plugin init",
		"Publishing a marketplace?:       apm-go marketplace init",
	},
}

var pluginMode = initMode{
	plugin:        true,
	pluginFormat:  pluginModeClaude,
	defaultYesVer: "0.1.0",
	validateName:  pluginValidateName,
	introTitle:    "Setting up your APM plugin project",
	// commands/init.py:291 uses the shared consumer success title even
	// when _perform_init is called through `plugin init`.
	successTitle: "APM project initialized successfully!",
	nextSteps:    pluginNextSteps,
}

// agentPluginMode is pluginMode with plugin_mode="agent" (commands/
// plugin/init.py:62): same name rule, version default, apm.yml shape and
// next steps, but additionally scaffolds mcp.json.
var agentPluginMode = initMode{
	plugin:        true,
	pluginFormat:  pluginModeAgent,
	defaultYesVer: "0.1.0",
	validateName:  pluginValidateName,
	introTitle:    "Setting up your APM plugin project",
	// commands/init.py:291 uses the shared consumer success title even
	// when _perform_init is called through `plugin init`.
	successTitle: "APM project initialized successfully!",
	nextSteps:    pluginNextSteps,
}

// pluginNextSteps is shared by both plugin modes (commands/init.py:322-327),
// apm-go spelled in place of apm.
var pluginNextSteps = []string{
	"Add dev dependencies:    apm-go install --dev <owner>/<repo>",
	"Pack as Agent Plugins v1:             apm-go pack --format agent-plugin",
	"Pack as Claude plugin:                apm-go pack --format claude-plugin",
}

// pluginNameRe is R3.3.a's kebab-case validation, matching upstream's
// commands/_helpers.py:610-617 exactly: lowercase letters/digits/hyphens,
// starting with a lowercase letter, 1-64 characters total.
var pluginNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// consumerValidateName is consumer init's existing, unchanged name check
// (R3.3.a: this must not become stricter or looser as a side effect of
// plugin mode's kebab-case rule, AC32/AC37).
func consumerValidateName(pn string) error {
	if strings.ContainsAny(pn, "/\\") || pn == ".." {
		return fmt.Errorf("invalid project name %q", pn)
	}
	return nil
}

func pluginValidateName(pn string) error {
	if !pluginNameRe.MatchString(pn) {
		return fmt.Errorf("invalid plugin name %q: must be lowercase letters, digits, and hyphens, starting with a lowercase letter (max 64 characters)", pn)
	}
	return nil
}

func initCmd() *cobra.Command {
	var (
		yes        bool
		targetFlag string
		force      bool
	)

	cmd := &cobra.Command{
		Use:          "init [project-name]",
		Short:        "Initialize a new APM project",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInitCore(args, consumerMode, yes, targetFlag, force, false)
		},
	}
	setInitFormatFlagErrorFunc(cmd)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive prompts and use auto-detected defaults")
	cmd.Flags().StringVar(&targetFlag, "target", "", "Comma-separated target list (skip prompt)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing apm.yml (alias for --yes on overwrite)")
	return cmd
}

// runInitCore is the shared body `initCmd` and `pluginInitCmd` (plugin.go)
// both run (design.md §3), parameterized by mode. verbose is currently
// accepted but unused by the shared body -- plugin init's own -v/--verbose
// flag (R3.3.f) exists for upstream CLI-surface parity; wiring it to extra
// output is not required by any AC in this task.
func runInitCore(args []string, mode initMode, yes bool, targetFlag string, force bool, verbose bool) error {
	// originalCwd is the directory the command was invoked from, captured
	// before Phase 1 possibly creates and enters a new project
	// subdirectory. R4's plugin-native-root warning inspects THIS
	// directory, not the freshly created (necessarily empty) project
	// subdirectory -- "project root" means the directory that already has
	// agents/skills/etc., which is where the user ran the command.
	originalCwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine directory: %w", err)
	}

	// Every prompt and progress record of an interactive run belongs to one
	// clack transcript (issue #14). Create the frame before Phase 1 so the
	// named-project progress record has the same gutter as the later form,
	// confirmation, and success records. Non-interactive runs keep the plain
	// stdout path unchanged.
	var ck *ux.Clack
	if !yes && ux.CanPrompt() {
		ck = ux.NewClack(os.Stderr)
		fmt.Fprintln(os.Stderr)
		ck.Banner(apmGoBanner)
		ck.Intro(mode.introTitle)
		ck.Detail("Press ^C at any time to quit.")
		ck.Bar()
	}

	// Phase 1: Project name resolution
	if len(args) > 0 && args[0] != "." {
		pn := args[0]
		if err := mode.validateName(pn); err != nil {
			return err
		}
		if err := os.MkdirAll(pn, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
		if err := os.Chdir(pn); err != nil {
			return fmt.Errorf("chdir: %w", err)
		}
		// commands/init.py:169-174 emits this progress record after the
		// named project directory is entered. The pinned Oracle currently
		// emits it even when mkdir(exist_ok=True) found an existing directory;
		// preserve that observed behavior.
		if ck != nil {
			ck.Detail(fmt.Sprintf("Created project directory: %s", pn))
		} else {
			ux.Running(os.Stdout, "Created project directory: %s", pn)
		}
	} else if mode.plugin {
		// R3.2/AC31: PROJECT-NAME is optional; when omitted, plugin mode
		// still validates the implied name (the current directory's
		// basename) against the kebab-case rule.
		if err := mode.validateName(filepath.Base(originalCwd)); err != nil {
			return err
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine directory: %w", err)
	}

	// Phase 2: Existing generated-files check (commands/init.py:195-215).
	// apm.yml drives the overwrite prompt; plugin modes additionally report
	// plugin.json (and mcp.json in agent mode) in the notice.
	_, existsErr := os.Stat("apm.yml")
	apmYmlExists := existsErr == nil
	existingGenerated := existingGeneratedFiles(mode)

	// Finding 4 (F09): gate on ANY generated file, not just apm.yml
	// (commands/init.py:205-215). Upstream's notice is "apm.yml already
	// exists" when that is the only one, else "Generated files already
	// exist: <list>".
	if len(existingGenerated) > 0 {
		rendered := strings.Join(existingGenerated, ", ")
		notice := "Generated files already exist: " + rendered
		if len(existingGenerated) == 1 && existingGenerated[0] == "apm.yml" {
			notice = "apm.yml already exists"
		}
		switch {
		case yes || force:
			if ck != nil {
				ck.Step(notice, "Overwriting (--force)")
				ck.Bar()
			} else {
				// commands/init.py:205-209: warning first, then progress.
				ux.Warn(os.Stderr, "%s", notice)
				ux.Info(os.Stderr, "--yes specified, overwriting: %s", rendered)
			}
		case ck != nil:
			ok, err := ck.Confirm(notice+". Continue and overwrite?", false)
			if err != nil {
				return fmt.Errorf("confirm overwrite: %w", err)
			}
			if !ok {
				ck.Outro("Initialization cancelled.")
				return nil
			}
			ck.Bar()
		default:
			return fmt.Errorf("%s; use --yes to overwrite", notice)
		}
	}

	var name, version, description, author string

	// Phase 3: Metadata collection
	if yes || !ux.CanPrompt() {
		name = filepath.Base(cwd)
		version = mode.defaultYesVer
		description = fmt.Sprintf("APM project for %s", name)
		author = manifest.DetectAuthor()
	} else {
		defaultName := filepath.Base(cwd)
		defaultDesc := fmt.Sprintf("APM project for %s", defaultName)
		fields := []ux.Field{
			{Key: "name", Label: "Project name", Default: defaultName},
			{Key: "version", Label: "Version", Default: "1.0.0"},
			{Key: "description", Label: "Description", Default: defaultDesc},
			{Key: "author", Label: "Author", Default: manifest.DetectAuthor()},
		}
		vals, formErr := ck.Form("Project metadata", fields)
		if formErr != nil {
			return fmt.Errorf("read project metadata: %w", formErr)
		}
		ck.Bar()
		name = vals["name"]
		version = vals["version"]
		description = vals["description"]
		author = vals["author"]
		// A-MAJOR-1 (external audit): Phase 1 only validates the name when
		// it comes from the explicit PROJECT-NAME arg, or (plugin mode)
		// the current directory's basename when the arg is omitted. It
		// never validated a name the user typed into THIS interactive
		// form, so `apm-go plugin init my-plugin` (a valid arg) followed by
		// editing the form's prefilled "my-plugin" to "My_Plugin" wrote an
		// invalid plugin name straight into apm.yml/plugin.json with no
		// error. Validate the form's final value the same way Phase 1
		// validates the arg/cwd-basename path.
		if err := mode.validateName(name); err != nil {
			return err
		}
		// The grouped form can't recompute the description default
		// reactively, so if the user changed the name but left the
		// prefilled description untouched, keep it in sync with the
		// entered name (avoids silently writing "APM project for <cwd>"
		// when the project was renamed).
		if description == defaultDesc && name != defaultName {
			description = fmt.Sprintf("APM project for %s", name)
		}
	}

	// Phase 4: Target selection
	var selectedTargets []string

	if targetFlag != "" {
		supported := make(map[string]bool)
		for _, s := range manifest.SupportedTargets {
			supported[s] = true
		}
		for _, t := range strings.Split(targetFlag, ",") {
			t = strings.TrimSpace(t)
			if !supported[t] {
				return fmt.Errorf("target %q is not supported by init; allowed: %s",
					t, strings.Join(manifest.SupportedTargets, ", "))
			}
			selectedTargets = append(selectedTargets, t)
		}
	} else if yes || !ux.CanPrompt() {
		selectedTargets = manifest.DetectTargets(cwd)
	} else {
		var existingTargets []string
		if apmYmlExists {
			existingTargets = readExistingTargets()
		}
		detected := manifest.DetectTargets(cwd)
		selectedTargets, err = interactiveTargetSelect(ck, detected, existingTargets)
		if err != nil {
			return fmt.Errorf("select targets: %w", err)
		}
		ck.Bar()
	}

	// Phase 5: Confirmation
	if ck != nil {
		body := []string{
			fmt.Sprintf("name:        %s", name),
			fmt.Sprintf("version:     %s", version),
			fmt.Sprintf("description: %s", description),
			fmt.Sprintf("author:      %s", author),
		}
		if len(selectedTargets) > 0 {
			body = append(body, fmt.Sprintf("targets:     %s", strings.Join(selectedTargets, ", ")))
		} else {
			body = append(body, "targets:     (none — auto-detect at compile time)")
		}
		ck.Note("About to create", body)
		ck.Bar()

		ok, err := ck.Confirm("Is this OK?", true)
		if err != nil {
			return fmt.Errorf("confirm creation: %w", err)
		}
		if !ok {
			ck.Outro("Aborted.")
			return nil
		}
	}

	// Oracle commands/init.py:249 starts the actual initialization phase
	// after confirmation and before its native-source warning/file writes.
	if ck != nil {
		ck.Detail(fmt.Sprintf("Initializing APM project: %s", name))
	} else {
		ux.Running(os.Stdout, "Initializing APM project: %s", name)
	}
	// R4: plugin-native root directory warning, shared by both modes
	// (R4.1). Purely informational -- it never blocks (R4.3, AC14 requires
	// exit 0). It follows the Oracle's start/progress ordering.
	if sources := detectPluginNativeRoot(originalCwd); len(sources) > 0 {
		msg := fmt.Sprintf("Found plugin-native content (%s) in this directory with no .apm/ -- `apm-go pack` will bundle it directly.", strings.Join(sources, ", "))
		if ck != nil {
			ck.Warn("%s", msg)
			ck.Bar()
		} else {
			ux.Warn(os.Stderr, "%s", msg)
		}
	}

	// Phase 6: File generation. buildManifestNode assembles the
	// document in semantic key order (R2); the validation
	// pipeline below dumps it FIRST so the bytes it validates
	// (via a round-trip through SafeLoad/ParseManifest) are
	// exactly the bytes written to disk (design.md §2).
	node := buildManifestNode(manifestSpec{
		Name:        name,
		Version:     version,
		Description: description,
		Author:      author,
		Targets:     selectedTargets,
		Plugin:      mode.plugin,
	})
	out, err := yamlcore.SafeDumpManifest(node)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}
	reloaded, err := yamlcore.SafeLoad(out)
	if err != nil {
		return fmt.Errorf("generated manifest is invalid: %w", err)
	}
	if _, _, err := manifest.ParseManifest(reloaded); err != nil {
		return fmt.Errorf("generated manifest fails validation: %w", err)
	}

	if !mode.plugin {
		if err := os.WriteFile("apm.yml", out, 0644); err != nil {
			return fmt.Errorf("write apm.yml: %w", err)
		}
	} else if err := writePluginScaffold(cwd, mode, out, name, version, description, author); err != nil {
		return err
	}

	// Phase 7: Success output
	if ck != nil {
		ck.Bar()
		clackRenderer(ck, mode.successTitle, buildInitSuccessContent(mode, cwd))
		ck.Outro("Done!")
		return nil
	}
	renderInitSuccess(mode, cwd)
	return nil
}

type initSuccessContent struct {
	files      []string
	nextSteps  []string
	agentrcTip string
	codexTip   string
	docsLine   string
}

// renderInitSuccess emits the shared success surface for non-interactive
// `init` and `plugin init`. The content is computed once, then handed to the
// Oracle stdout renderer; the interactive path hands the same content to
// clackRenderer so the two modes cannot drift (commands/init.py:291-400).
func renderInitSuccess(mode initMode, projectRoot string) {
	oracleStdoutRenderer(mode.successTitle, buildInitSuccessContent(mode, projectRoot))
}

// buildInitSuccessContent computes the words shared by the Oracle-style
// stdout renderer and the interactive clack renderer. Conditional tips use
// the same project-root checks as the former renderInitSuccess body.
func buildInitSuccessContent(mode initMode, projectRoot string) initSuccessContent {
	content := initSuccessContent{
		files:     []string{"apm.yml"},
		nextSteps: append([]string(nil), mode.nextSteps...),
		docsLine:  "Docs: https://microsoft.github.io/apm  |  Star: https://github.com/microsoft/apm",
	}
	if mode.plugin {
		content.files = append(content.files, "plugin.json")
		if mode.pluginFormat == pluginModeAgent {
			content.files = append(content.files, "mcp.json")
		}
	}

	if !mode.plugin {
		agentrcInstalled, hasInstructions := detectAgentrc(projectRoot)
		if !hasInstructions {
			if agentrcInstalled {
				content.nextSteps = append(content.nextSteps[:1], append([]string{
					"Generate agent instructions:     agentrc init",
				}, content.nextSteps[1:]...)...)
			} else {
				content.agentrcTip = "Tip: Use agentrc to generate tailored agent instructions from your codebase. https://github.com/microsoft/agentrc"
			}
		}
	}
	if info, err := os.Stat(filepath.Join(projectRoot, ".codex")); err == nil && info.IsDir() {
		content.codexTip = "Tip: Use '--target agent-skills' to also deploy skills to .agents/skills/ for other clients."
	}
	return content
}

// oracleStdoutRenderer preserves the pinned Oracle's non-interactive output
// bytes: its table, box, tips, and footer remain on stdout for --yes and
// non-TTY runs.
func oracleStdoutRenderer(title string, content initSuccessContent) {
	ux.Sparkle(os.Stdout, "%s", title)

	rows := make([][]string, 0, len(content.files))
	for _, file := range content.files {
		rows = append(rows, []string{"*", file})
	}
	// _create_files_table receives (*, filename, description) tuples in the
	// Oracle, but its two-column renderer uses only tuple positions 0 and 1
	// (utils/console.py:194-212). Keep that observable "*" marker layout.
	ux.Plain(os.Stdout, "    Created Files")
	ux.Table(os.Stdout, []string{"File", "Description"}, rows)
	ux.Plain(os.Stdout, "")

	body := make([]string, len(content.nextSteps))
	for i, step := range content.nextSteps {
		body[i] = "* " + step
	}
	ux.Box(os.Stdout, "Next Steps", body)
	if content.agentrcTip != "" {
		ux.Info(os.Stdout, "%s", content.agentrcTip)
	}
	if content.codexTip != "" {
		ux.Info(os.Stdout, "%s", content.codexTip)
	}
	ux.Plain(os.Stdout, "  %s", content.docsLine)
}

// clackRenderer renders the same success words as one completed transcript
// step. Clack supplies the frame and prefixes every body line with its
// gutter, so no Oracle table/panel drawing can leak outside the interactive
// transcript.
func clackRenderer(ck *ux.Clack, title string, content initSuccessContent) {
	body := []string{
		"Created files: " + strings.Join(content.files, ", "),
		"Next steps:",
	}
	body = append(body, content.nextSteps...)
	if content.agentrcTip != "" {
		body = append(body, content.agentrcTip)
	}
	if content.codexTip != "" {
		body = append(body, content.codexTip)
	}
	body = append(body, content.docsLine)
	ck.Step(title, strings.Join(body, "\n"))
}

// detectAgentrc mirrors init.py:40-55. shutil.which checks PATH without
// executing agentrc; the instruction artifacts suppress the suggestion when
// the project already has a root AGENTS.md, a Copilot instructions file, or
// an instructions directory.
func detectAgentrc(projectRoot string) (installed, hasInstructions bool) {
	_, err := exec.LookPath("agentrc")
	installed = err == nil
	hasInstructions = fileExists(filepath.Join(projectRoot, ".github", "copilot-instructions.md")) ||
		fileExists(filepath.Join(projectRoot, "AGENTS.md")) ||
		directoryExists(filepath.Join(projectRoot, ".github", "instructions"))
	return installed, hasInstructions
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// writePluginScaffold writes apm.yml plus R3.3.d's plugin.json template
// (distinct from `apm pack`'s apm.yml-synthesized one, see
// internal/pluginjson's doc comment) and, in agent mode, mcp.json -- as ONE
// staged commit (Finding 5, F03/F09; upstream commands/init.py:407-444), so a
// failure on any file restores every prior file and leaves no partial
// scaffold.
func writePluginScaffold(cwd string, mode initMode, apmYML []byte, name, version, description, author string) error {
	stage, err := pluginjson.NewStagedScaffold(cwd)
	if err != nil {
		return err
	}
	defer stage.Cleanup()

	if err := os.WriteFile(stage.Add("apm.yml"), apmYML, 0644); err != nil {
		return fmt.Errorf("write apm.yml: %w", err)
	}
	agent := mode.pluginFormat == pluginModeAgent
	for _, f := range pluginjson.ScaffoldFiles(agent) {
		stage.Add(f)
	}
	scaffold := pluginjson.Scaffold
	if agent {
		scaffold = pluginjson.ScaffoldAgent
	}
	if err := scaffold(stage.Dir(), name, version, description, author); err != nil {
		return err
	}
	return stage.Commit()
}

// existingGeneratedFiles returns, in upstream order (commands/init.py:
// 196-203), the generated files of mode that already exist in the cwd:
// apm.yml, then plugin.json (plugin modes), then mcp.json (agent mode).
func existingGeneratedFiles(mode initMode) []string {
	candidates := []string{"apm.yml"}
	if mode.plugin {
		candidates = append(candidates, "plugin.json")
		if mode.pluginFormat == pluginModeAgent {
			candidates = append(candidates, "mcp.json")
		}
	}
	var existing []string
	for _, f := range candidates {
		if _, err := os.Stat(f); err == nil {
			existing = append(existing, f)
		}
	}
	return existing
}

// interactiveTargetSelect prompts for the target list via a huh MultiSelect
// (space to toggle, matching huh's own default keybinding), pre-selecting
// every already-configured (existing) or auto-detected target. If the user
// confirms with nothing selected, it asks once more (via ux.Confirm) whether
// to proceed without pinning any target, looping back to the MultiSelect
// prompt otherwise.
//
// Any error from the underlying MultiSelect/Confirm prompts (e.g. the huh
// form is aborted with Ctrl-C) is returned to the caller immediately instead
// of being swallowed: a swallowed error previously left `selected` nil and
// `cont` at its zero value (false), which re-entered this function
// recursively on every aborted prompt -- an abort loop with no way out.
func interactiveTargetSelect(ck *ux.Clack, detected, existing []string) ([]string, error) {
	opts := targetSelectOptions(detected, existing)

	for {
		selected, err := ck.MultiSelect("Select targets for this project", opts)
		if err != nil {
			return nil, err
		}
		if len(selected) > 0 {
			return selected, nil
		}

		ck.Warn("No targets selected. APM will auto-detect targets from your filesystem on every compile.")
		cont, err := ck.Confirm("Continue without pinning targets?", true)
		if err != nil {
			return nil, err
		}
		if cont {
			return nil, nil
		}
	}
}

// targetSelectOptions builds the MultiSelect option list interactiveTargetSelect
// prompts with: one entry per manifest.PromptTargets (manifest.SupportedTargets
// minus manifest.ExplicitOnlyTargets -- parity with Python apm_cli's
// _PROMPT_TARGETS_ORDERED filtered by EXPLICIT_ONLY_TARGETS, commands/init.py:629,
// v0.26.0; explicit-only targets like antigravity/agent-skills stay selectable
// via --target but are not offered in the prompt), pre-selected when the
// target is already configured (existing) or auto-detected, and labeled with
// the detection signal when auto-detected. Split out from
// interactiveTargetSelect so tests (AC25) can inspect the option set
// actually offered to the user without driving a live prompt.
func targetSelectOptions(detected, existing []string) []ux.Option {
	checked := make(map[string]bool)
	for _, t := range existing {
		checked[t] = true
	}
	for _, t := range detected {
		checked[t] = true
	}

	detectedSet := make(map[string]bool)
	for _, t := range detected {
		detectedSet[t] = true
	}

	opts := make([]ux.Option, len(manifest.PromptTargets))
	for i, t := range manifest.PromptTargets {
		label := t
		if detectedSet[t] {
			for _, sig := range manifest.SignalWhitelist {
				if sig.Target == t {
					label = fmt.Sprintf("%s  (detected %s)", t, sig.Path)
					break
				}
			}
		}
		opts[i] = ux.Option{Label: label, Value: t, Selected: checked[t]}
	}
	return opts
}

// readExistingTargets reads the target selection out of an already-written
// apm.yml, so interactiveTargetSelect can pre-select it (AC2/AC3). It tries
// the same yamlcore.SafeLoad -> manifest.ParseManifest pipeline install/pack
// use to produce m.Target first, instead of a second, ad hoc parser: a bare
// type-switch over a raw yaml.Unmarshal decode (the prior implementation)
// only handled a plain list or a single bare scalar, and silently dropped
// every other form ParseManifest's parseTargetField/parseTargetsField
// already accept -- CSV sugar on the singular scalar key ("target:
// claude,copilot", manifest.go's parseTargetField) and target aliases (e.g.
// "vscode" normalizing to "copilot" via ValidateTarget).
//
// 2026-07-30 codex Tier 2 B5: ParseManifest fails the WHOLE document on any
// schema violation, even one unrelated to targets (e.g. a missing required
// version: field) -- an apm.yml that is otherwise perfectly readable for its
// target: value then loses its ENTIRE preselection, which is a regression
// against the pre-ParseManifest ad hoc parser this function replaced (that
// one could still read target: out of a version-less file). So a
// ParseManifest failure now falls back to lenientReadTargets, which reads
// only the target/targets key directly and drops individual unparseable
// tokens instead of giving up on the whole file.
func readExistingTargets() []string {
	data, err := os.ReadFile("apm.yml")
	if err != nil {
		return nil
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil
	}
	if m, _, err := manifest.ParseManifest(node); err == nil {
		return m.Target
	}
	return lenientReadTargets(node)
}

// lenientReadTargets is readExistingTargets' fallback whenever
// manifest.ParseManifest returns an error -- readExistingTargets (line 335)
// calls it unconditionally on any ParseManifest failure, not only one
// unrelated to targets. An earlier version of this comment claimed the
// opposite ("a reason unrelated to targets"); that claim does not hold: a
// failure caused BY the target/targets value itself is exactly as fatal to
// ParseManifest as an unrelated one, because manifest.go's single
// range-and-return loop (manifest.go:100-196) returns on the first error it
// hits, before reaching later checks like the required version: field
// (manifest.go:204). Concretely, `targets: [claude, {foo: bar}]` with a
// valid version: fails ParseManifest with "unknown target" for the mapping
// element (targetTokenFromNode/ValidateTarget via parseTargetsField,
// manifest.go:298-317) -- a targets-related failure -- and this function is
// still the one that recovers the still-legal "claude" entry (verified
// directly: readExistingTargets() on that fixture returns [claude]).
//
// So this function reads the target/targets key directly off the raw node
// instead of going through parseTargetField/parseTargetsField (both
// unexported, and both HARD ERROR on an unparseable/empty targets: block,
// which is the wrong behavior for a best-effort preselection reader): each
// token is normalized via manifest.ValidateTarget and a token that fails
// validation is dropped rather than aborting the whole read -- this is also
// how a targets-related ParseManifest failure (one bad token) still yields a
// partial, useful preselection instead of losing it entirely. Both target:
// and targets: present at once is invalid apm.yml (manifest.go's
// hasConflictingTargetKeys) -- this does not guess a winner, matching
// ParseManifest's own refusal.
//
// 2026-07-30 codex Tier 2: the singular target: scalar and the plural
// targets: scalar are NOT parsed the same way by the real parser, so this
// fallback must not treat them the same either. manifest.go's
// parseTargetField (target:, manifest.go:265) splits a scalar on "," --
// "target: claude,copilot" is CSV sugar for two targets. manifest.go's
// parseTargetsField (targets:, manifest.go:312-313) does NOT split a scalar
// on "," -- "targets: claude,copilot" is a single-element list whose one
// element happens to contain a comma, i.e. one (invalid) token, not CSV
// sugar. An earlier version of this function CSV-split every scalar
// unconditionally, so a plural "targets: claude,copilot" was silently
// manufactured into two preselected targets an interactive init() run never
// actually wrote for that key. Whether to split is now keyed off which of
// target:/targets: was actually present (isSingular), not the node kind
// alone.
func lenientReadTargets(doc *yamllib.Node) []string {
	if doc.Kind != yamllib.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yamllib.MappingNode {
		return nil
	}

	var targetVal, targetsVal *yamllib.Node
	for i := 0; i < len(root.Content)-1; i += 2 {
		switch root.Content[i].Value {
		case "target":
			targetVal = root.Content[i+1]
		case "targets":
			targetsVal = root.Content[i+1]
		}
	}
	if targetVal != nil && targetsVal != nil {
		return nil
	}
	val := targetVal
	isSingular := true
	if val == nil {
		val = targetsVal
		isSingular = false
	}
	if val == nil {
		return nil
	}

	var rawTokens []string
	switch val.Kind {
	case yamllib.ScalarNode:
		if raw := strings.TrimSpace(val.Value); raw != "" {
			if isSingular {
				rawTokens = strings.Split(raw, ",")
			} else {
				rawTokens = []string{raw}
			}
		}
	case yamllib.SequenceNode:
		for _, item := range val.Content {
			if item.Kind == yamllib.ScalarNode {
				rawTokens = append(rawTokens, item.Value)
			}
		}
	}

	var targets []string
	for _, tok := range rawTokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		normalized, err := manifest.ValidateTarget(tok)
		if err != nil {
			continue
		}
		targets = append(targets, normalized)
	}
	return targets
}
