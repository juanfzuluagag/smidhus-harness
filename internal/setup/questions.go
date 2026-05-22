package setup

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"harness-cli/internal/ui"
)

// ---------------------------------------------------------------------------
// UI Theme
// ---------------------------------------------------------------------------

// getTheme returns a customized huh theme matching the Coco Clean aesthetic.
func getTheme() *huh.Theme {
	t := huh.ThemeCharm()
	t.Focused.Title = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary)).Bold(true)
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary)).Bold(true)
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Text))
	t.Focused.FocusedButton = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color(ui.Primary)).Bold(true).Padding(0, 1)
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary))
	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary))
	return t
}

// ---------------------------------------------------------------------------
// Phase 0: Model fetching & selection
// ---------------------------------------------------------------------------

// fetchAvailableModels runs `opencode models` silently and returns a
// provider → model-list map.
func fetchAvailableModels() (map[string][]string, error) {
	out, err := exec.Command("opencode", "models").Output()
	if err != nil {
		return nil, fmt.Errorf("could not run 'opencode models': %w", err)
	}

	modelMap := make(map[string][]string)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Clean up numerical prefixes like "[1] "
		if strings.HasPrefix(line, "[") {
			idx := strings.Index(line, "] ")
			if idx != -1 {
				line = strings.TrimSpace(line[idx+2:])
			}
		}

		parts := strings.SplitN(line, "/", 2)
		var provider, modelName string
		if len(parts) == 2 {
			provider = parts[0]
			modelName = line // keep full string like google/gemini-...
		} else {
			provider = "other"
			modelName = line
		}
		modelMap[provider] = append(modelMap[provider], modelName)
	}
	return modelMap, nil
}

// selectModel presents a 2-step sequential flow: provider → model.
func selectModel(modelMap map[string][]string) (string, error) {
	var providers []string
	for p := range modelMap {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	// Step A: Provider
	var selectedProvider string
	err := huh.NewSelect[string]().
		Title("Step 1: Select Principal AI Provider").
		Options(huh.NewOptions(providers...)...).
		Value(&selectedProvider).
		Height(20).
		WithTheme(getTheme()).
		Run()
	if err != nil {
		return "", err
	}

	models := modelMap[selectedProvider]
	sort.Strings(models)

	// Step B: Model
	var selectedModel string
	err = huh.NewSelect[string]().
		Title(fmt.Sprintf("Step 2: Select Principal Model for %s", selectedProvider)).
		Options(huh.NewOptions(models...)...).
		Value(&selectedModel).
		Height(20).
		WithTheme(getTheme()).
		Run()
	if err != nil {
		return "", err
	}

	return selectedModel, nil
}

// askThinkingBudget presents a flow to select the global thinking budget.
func askThinkingBudget() (int, error) {
	var budget int
	budgetOpts := []huh.Option[int]{
		huh.NewOption("Default (Model decides)", 0),
		huh.NewOption("Low (Fast responses)", 1000),
		huh.NewOption("Medium (Balanced)", 4000),
		huh.NewOption("High (Deep reflection)", 10000),
	}

	err := huh.NewSelect[int]().
		Title("Reasoning Effort / Thinking Budget").
		Options(budgetOpts...).
		Value(&budget).
		Height(20).
		WithTheme(getTheme()).
		Run()
	return budget, err
}

// askBlueprintMode asks whether to use AI or manual mode for blueprint
// generation.
func askBlueprintMode() (bool, error) {
	var useAI bool
	err := huh.NewConfirm().
		Title("Blueprint Generation Mode").
		Description("Do you want the AI to expand and complement the data? (No = Manual literal transcription)").
		Affirmative("Yes, AI Mode").
		Negative("No, Manual").
		Value(&useAI).
		WithTheme(getTheme()).
		Run()
	return useAI, err
}

// ---------------------------------------------------------------------------
// Phase 1A: Questionnaire (blank project or manual choice)
// ---------------------------------------------------------------------------

func runQuestionnaire(defaultName string) (string, error) {
	var name, stack, depMgr, arch, security, uiux string

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Project Name").Value(&name).Placeholder(defaultName),
			huh.NewInput().Title("Main Tech Stack (Language/Framework)").Value(&stack),
			huh.NewInput().Title("Dependency Manager (npm, go modules, pip...)").Value(&depMgr),
			huh.NewInput().Title("Architecture & Patterns (Clean Architecture, MVC...)").Value(&arch),
			huh.NewInput().Title("Security Requirements (Auth, OWASP...)").Value(&security),
			huh.NewInput().Title("UI/UX Guidelines (Design system, colors - blank if N/A)").Value(&uiux),
		),
	).WithTheme(getTheme()).Run()

	if err != nil {
		return "", err
	}

	if name == "" {
		name = defaultName
	}

	block := fmt.Sprintf(
		"Name: %s\nStack: %s\nDependency Manager: %s\nArchitecture: %s\nSecurity: %s\nUI/UX: %s",
		name, stack, depMgr, arch, security, uiux,
	)
	return block, nil
}
