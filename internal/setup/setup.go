package setup

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"harness-cli/internal/scanner"
	"harness-cli/internal/ui"
)

//go:embed templates/*.md templates/*.txt
var TemplatesFS embed.FS

// ---------------------------------------------------------------------------
// Dependency Check
// ---------------------------------------------------------------------------

// CheckOpenCode verifies that the `opencode` binary is available in PATH.
// We fail immediately rather than deferring to exec so the error is clear.
func CheckOpenCode() error {
	_, err := exec.LookPath("opencode")
	if err != nil {
		ui.PrintError("'opencode' is not installed or not found in your $PATH.\n   It is a mandatory dependency for smidhus-harness.\n   Install it and try again: https://opencode.ai")
		os.Exit(1)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Public Entry Point
// ---------------------------------------------------------------------------

// InitProject runs the full interactive harness initialization flow for
// targetPath, generating .harness/blueprint.md, agents.yml, and tasks.json.
// The function is intentionally sequential — each phase feeds the next.
func InitProject(targetPath string, runCmdCreator func(*tea.Program, chan struct{}) tea.Cmd) error {
	ui.PrintBanner()

	harnessDir := filepath.Join(targetPath, ".harness")

	// Perform shallow scan on target project path
	shallowTree, err := scanner.ShallowScan(targetPath, 3)
	if err != nil {
		return fmt.Errorf("could not scan project directory tree: %w", err)
	}

	// Create folder skeleton for state and spec artifacts.
	for _, d := range []string{
		filepath.Join(harnessDir, "state"),
		filepath.Join(harnessDir, "specs"),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("error creating folder %s: %w", d, err)
		}
	}

	// Drop placeholder files so the specs/ directory is self-documenting.
	readmeContent := `# Project Specifications (Specs)

Welcome to the specifications directory. The Architect agent will generate design files here. You can also manually add .md files to request new features.`
	os.WriteFile(filepath.Join(harnessDir, "specs", "README.md"), []byte(readmeContent), 0644)

	templateContent := `# Specification Template (000_feature_name.md)

## 1. Overview & Context
- **Objective**: What is the goal of this specification?

## 2. Functional Requirements
- [ ] Requirement 1: ...

## 3. Technical Architecture & Constraints
- **Files to Create/Modify**: ...`
	os.WriteFile(filepath.Join(harnessDir, "specs", "template_spec.md"), []byte(templateContent), 0644)

	// Fetch models before showing any prompts to avoid a mid-questionnaire
	// failure leaving the user with no fallback option.
	modelMap, err := fetchAvailableModels()
	if err != nil || len(modelMap) == 0 {
		ui.PrintError("Could not retrieve model list. Falling back to default model.")
		modelMap = map[string][]string{
			"google": {"google/gemini-2.5-flash"},
		}
	}

	blank, err := isBlankProject(targetPath)
	if err != nil {
		return fmt.Errorf("could not scan project directory: %w", err)
	}

	absPath, _ := filepath.Abs(targetPath)
	defaultName := filepath.Base(absPath)

	// Pre-generate standard contextData for non-blank project in case user selects AI Scan
	var initialContextData string
	if !blank {
		var readmeContent []byte
		readmePath := filepath.Join(targetPath, "README.md")
		if _, err := os.Stat(readmePath); err == nil {
			readmeContent, _ = os.ReadFile(readmePath)
		} else {
			readmePathLower := filepath.Join(targetPath, "readme.md")
			if _, err := os.Stat(readmePathLower); err == nil {
				readmeContent, _ = os.ReadFile(readmePathLower)
			}
		}
		initialContextData = fmt.Sprintf("## Directory Structure\n```\n%s```\n## README\n%s\n", shallowTree, string(readmeContent))
	}

	var p *tea.Program

	execFunc := func(selModel string, budget int, aiMode bool, contextData string) (string, error) {
		// 1. Generate blueprint
		if aiMode {
			if err := generateBlueprintAI(selModel, contextData, harnessDir, p); err != nil {
				return "", err
			}
		} else {
			if err := generateBlueprintManual(contextData, harnessDir); err != nil {
				return "", err
			}
		}

		// Sync the actual local directory tree into the generated blueprint
		blueprintPath := filepath.Join(harnessDir, "blueprint.md")
		if err := scanner.SyncBlueprintTree(blueprintPath, shallowTree); err != nil {
			return "", err
		}

		// 2. Wire up agents.yml and seed the first task.
		if err := generateAgentsAI(selModel, budget, modelMap, harnessDir, p); err != nil {
			return "", err
		}

		if err := writeTasksJSON(harnessDir, defaultName); err != nil {
			return "", err
		}

		// Read final quickstart guide
		guideRaw, err := TemplatesFS.ReadFile("templates/post_init_guide.txt")
		if err != nil {
			return "", err
		}
		return string(guideRaw), nil
	}

	tuiModel := ui.NewInitTUIModel(modelMap, targetPath, blank, defaultName, initialContextData, execFunc)
	p = tea.NewProgram(tuiModel, tea.WithAltScreen())
	tuiModel.RunCmd = runCmdCreator(p, tuiModel.RetryChan)

	ui.TUILogCallback = func(msg string) {
		p.Send(ui.LogMsg(msg + "\n"))
	}

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	ui.TUILogCallback = nil

	if tuiModel.InitErr() != nil {
		return tuiModel.InitErr()
	}

	return nil
}