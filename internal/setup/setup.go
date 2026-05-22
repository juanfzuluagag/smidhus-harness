package setup

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
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
func InitProject(targetPath string) error {
	ui.PrintBanner()

	harnessDir := filepath.Join(targetPath, ".harness")

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

	selectedModel, err := selectModel(modelMap)
	if err != nil {
		return fmt.Errorf("model selection failed: %w", err)
	}

	budget, err := askThinkingBudget()
	if err != nil {
		return fmt.Errorf("budget selection failed: %w", err)
	}

	aiMode, err := askBlueprintMode()
	if err != nil {
		return fmt.Errorf("mode selection failed: %w", err)
	}

	// Collect context: blank projects go straight to the questionnaire;
	// existing codebases can choose between AI scan and manual input.
	blank, err := isBlankProject(targetPath)
	if err != nil {
		return fmt.Errorf("could not scan project directory: %w", err)
	}

	var contextData string
	absPath, _ := filepath.Abs(targetPath)
	defaultName := filepath.Base(absPath)

	if blank {
		contextData, err = runQuestionnaire(defaultName)
		if err != nil {
			return fmt.Errorf("questionnaire error: %w", err)
		}
	} else {
		var useAI bool
		err := huh.NewConfirm().
			Title("Advanced Project Detected").
			Description("This directory contains code. Do you want the AI to scan the architecture? (Warning: Consumes more tokens)").
			Affirmative("Yes, AI Scan").
			Negative("No, Manual Questionnaire").
			Value(&useAI).
			WithTheme(getTheme()).
			Run()

		if err != nil {
			return err
		}

		if useAI && aiMode {
			contextData = collectRepoXray(targetPath)
		} else {
			contextData, err = runQuestionnaire(defaultName)
			if err != nil {
				return fmt.Errorf("questionnaire error: %w", err)
			}
		}
	}

	// Generate blueprint: AI mode sends collected context to opencode;
	// manual mode formats it into a plain markdown scaffold.
	if aiMode {
		if err := generateBlueprintAI(selectedModel, contextData, harnessDir); err != nil {
			return fmt.Errorf("AI blueprint generation failed: %w", err)
		}
	} else {
		if err := generateBlueprintManual(contextData, harnessDir); err != nil {
			return fmt.Errorf("manual blueprint generation failed: %w", err)
		}
	}

	// Wire up agents.yml and seed the first task.
	if err := generateAgentsAI(selectedModel, budget, modelMap, harnessDir); err != nil {
		return fmt.Errorf("could not generate agents.yml: %w", err)
	}
	if err := writeTasksJSON(harnessDir, defaultName); err != nil {
		return fmt.Errorf("could not write tasks.json: %w", err)
	}

	// ── Final message: read from embedded template ──────────────────────
	guideRaw, err := TemplatesFS.ReadFile("templates/post_init_guide.txt")
	if err != nil {
		return fmt.Errorf("could not read post-init guide: %w", err)
	}
	fmt.Println("\n" + ui.RenderMessage(strings.TrimSpace(string(guideRaw))))
	return nil
}