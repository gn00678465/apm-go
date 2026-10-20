package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/apm-go/apm/internal/yamlcore"
)

type copilotAdapter struct{}

func (a *copilotAdapter) Name() string { return "copilot" }

func (a *copilotAdapter) DeployRoots() []string { return []string{".github/", ".agents/"} }

func (a *copilotAdapter) SupportedTypes() []PrimitiveType {
	return []PrimitiveType{TypeInstructions, TypePrompts, TypeAgents, TypeSkills, TypeHooks}
}

func (a *copilotAdapter) skillRoot(Primitive) string { return sharedSkillRoot }

func (a *copilotAdapter) checkInstruction(p Primitive, deployDir string) error {
	return checkFileDest(copilotInstructionPath(p), deployDir)
}

func copilotInstructionPath(p Primitive) string {
	return fmt.Sprintf(".github/instructions/%s.instructions.md", p.Name)
}

func (a *copilotAdapter) DeployPrimitive(p Primitive, projectDir string) ([]string, error) {
	switch p.Type {
	case TypeSkills:
		return deploySkillTo(p, projectDir, a.skillRoot(p))
	case TypeInstructions:
		return deployFileToPath(p, copilotInstructionPath(p), projectDir)
	case TypePrompts:
		return deployFileToPath(p, fmt.Sprintf(".github/prompts/%s.prompt.md", p.Name), projectDir)
	case TypeAgents:
		return deployFileToPath(p, fmt.Sprintf(".github/agents/%s.agent.md", p.Name), projectDir)
	case TypeHooks:
		return deployFileToPath(p, ".github/hooks/"+copilotHookFile(p)+".json", projectDir)
	default:
		return nil, nil
	}
}

// copilotHookFile is the stem of the file a copilot hook is written to,
// <package>-<stem> (integration/hook_integrator.py:1065-1067). A dependency's
// package is its directory below apm_modules (install_path.name, :901), an
// alias when it has one (measured, issue151-impl/al3-or/aliashook).
func copilotHookFile(p Primitive) string {
	pkg := activeDeployMode.hookPackage
	if p.DepKey != "" {
		pkg = filepath.Base(p.PackageDir)
	} else if pkg == "" {
		pkg = localHookPackage
	}
	return pkg + "-" + p.Name
}

// localHookPackage is _safe_source_name of the package the Oracle builds of
// the project's own content, "_local" (install/services.py:742).
const localHookPackage = "local"

// projectHookPackage is the package of the project's own hooks: the name the
// apm.yml of root gives, made safe, else localHookPackage
// (_get_root_local_package_name, integration/hook_integrator.py:861-881).
// For --global root is HOME, the Oracle's project_root there.
func projectHookPackage(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "apm.yml"))
	if err != nil {
		return localHookPackage
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return localHookPackage
	}
	v, err := constructPy(doc)
	if err != nil {
		return localHookPackage
	}
	d, ok := v.(*pyDict)
	if !ok {
		return localHookPackage
	}
	if s, ok := d.get(pyStr("name")).(pyStr); ok {
		if safe := safeSourceName(string(s)); safe != "" {
			return safe
		}
	}
	return localHookPackage
}

var (
	unsafeSourceRun = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	dotRun          = regexp.MustCompile(`\.{2,}`)
)

// safeSourceName is the Oracle's _safe_source_name
// (integration/hook_integrator.py:844-855), "" where it returns its fallback.
func safeSourceName(value string) string {
	safe := unsafeSourceRun.ReplaceAllString(pyStrip(value), "-")
	safe = strings.Trim(dotRun.ReplaceAllString(safe, "."), ".-_")
	if safe == "." || safe == ".." {
		return ""
	}
	return safe
}
