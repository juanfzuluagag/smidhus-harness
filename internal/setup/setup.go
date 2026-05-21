package setup

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"harness-cli/internal/config"
)

//go:embed templates/*.md
var TemplatesFS embed.FS

// ---------------------------------------------------------------------------
// Dependency Check
// ---------------------------------------------------------------------------

// CheckOpenCode verifies that the `opencode` binary is available in $PATH.
func CheckOpenCode() error {
	_, err := exec.LookPath("opencode")
	if err != nil {
		return fmt.Errorf(
			"❌ 'opencode' is not installed or not found in your $PATH.\n" +
				"   It is a mandatory dependency for smidhus-harness.\n" +
				"   Install it and try again: https://opencode.ai",
		)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Phase 0: Model Selection
// ---------------------------------------------------------------------------

// fetchAvailableModels runs `opencode models` silently and returns the list.
func fetchAvailableModels() ([]string, error) {
	out, err := exec.Command("opencode", "models").Output()
	if err != nil {
		return nil, fmt.Errorf("could not run 'opencode models': %w", err)
	}

	var models []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			models = append(models, line)
		}
	}
	return models, nil
}

// selectModel presents a numbered menu of models and returns the chosen one.
func selectModel(reader *bufio.Reader, models []string) (string, error) {
	fmt.Println("\n┌─────────────────────────────────────────┐")
	fmt.Println("│        Available Models (opencode)      │")
	fmt.Println("└─────────────────────────────────────────┘")
	for i, m := range models {
		fmt.Printf("  [%d] %s\n", i+1, m)
	}
	fmt.Printf("\nEnter the number of the model to use: ")

	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)

	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx < 1 || idx > len(models) {
		return "", fmt.Errorf("invalid selection: %q", line)
	}
	return models[idx-1], nil
}

// askBlueprintMode asks whether to use AI or manual mode for blueprint generation.
// Returns true if AI mode is selected.
func askBlueprintMode(reader *bufio.Reader) (bool, error) {
	fmt.Println("\n┌─────────────────────────────────────────────────────────────┐")
	fmt.Println("│              Blueprint Generation Mode                      │")
	fmt.Println("├─────────────────────────────────────────────────────────────┤")
	fmt.Println("│  [1] AI Mode   — The AI expands and complements the data    │")
	fmt.Println("│  [2] Manual    — Strict literal transcription of answers    │")
	fmt.Println("└─────────────────────────────────────────────────────────────┘")
	fmt.Print("\nChoose a mode [1/2]: ")

	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(line) == "1", nil
}

// ---------------------------------------------------------------------------
// Phase 1: Project Scanning
// ---------------------------------------------------------------------------

// noiseFiles are entries to ignore when deciding if a directory is blank.
var noiseFiles = map[string]bool{
	".git": true, ".DS_Store": true, ".gitignore": true,
	".idea": true, ".vscode": true, "README.md": true, "readme.md": true,
	".harness": true,
}

// isBlankProject returns true if targetPath contains only noise files/dirs.
func isBlankProject(targetPath string) (bool, error) {
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !noiseFiles[e.Name()] {
			return false, nil
		}
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// Phase 1A: Questionnaire (blank project OR manual choice)
// ---------------------------------------------------------------------------

func askString(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := reader.ReadString('\n')
	return strings.TrimSpace(line), err
}

func runQuestionnaire(reader *bufio.Reader) (string, error) {
	fmt.Println("\n📋 Project Questionnaire")
	fmt.Println("────────────────────────────────────")

	name, err := askString(reader, "  Project name: ")
	if err != nil {
		return "", err
	}
	stack, err := askString(reader, "  Main tech stack (Language/Framework): ")
	if err != nil {
		return "", err
	}
	depMgr, err := askString(reader, "  Dependency manager (npm, go modules, pip…): ")
	if err != nil {
		return "", err
	}
	arch, err := askString(reader, "  Architecture & patterns (e.g. Clean Architecture, MVC): ")
	if err != nil {
		return "", err
	}
	security, err := askString(reader, "  Security requirements (auth, secrets, OWASP rules): ")
	if err != nil {
		return "", err
	}
	uiux, err := askString(reader, "  UI/UX guidelines (design system, colors — leave blank if N/A): ")
	if err != nil {
		return "", err
	}

	block := fmt.Sprintf(
		"Name: %s\nStack: %s\nDependency Manager: %s\nArchitecture: %s\nSecurity: %s\nUI/UX: %s",
		name, stack, depMgr, arch, security, uiux,
	)
	return block, nil
}

// ---------------------------------------------------------------------------
// Phase 1B: AI Scan (advanced project, AI mode)
// ---------------------------------------------------------------------------

// ignoreForTree are dirs/files to skip when building the tree snapshot.
var ignoreForTree = map[string]bool{
	"node_modules": true, ".git": true, "bin": true, "dist": true,
	"build": true, ".harness": true, "__pycache__": true, ".next": true,
	"vendor": true, "target": true,
}

// buildTree returns an indented directory tree up to maxDepth levels deep.
func buildTree(root string, depth, maxDepth int) string {
	if depth > maxDepth {
		return ""
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	indent := strings.Repeat("  ", depth)
	for _, e := range entries {
		if ignoreForTree[e.Name()] {
			continue
		}
		if e.IsDir() {
			sb.WriteString(fmt.Sprintf("%s📁 %s/\n", indent, e.Name()))
			sb.WriteString(buildTree(filepath.Join(root, e.Name()), depth+1, maxDepth))
		} else {
			sb.WriteString(fmt.Sprintf("%s📄 %s\n", indent, e.Name()))
		}
	}
	return sb.String()
}

// manifestFiles are key files whose content helps the AI understand the project.
var manifestFiles = []string{
	"package.json", "go.mod", "pom.xml", "Cargo.toml",
	"pyproject.toml", "requirements.txt", "composer.json",
}

const maxManifestBytes = 4096

func collectManifests(targetPath string) string {
	var sb strings.Builder
	for _, name := range manifestFiles {
		path := filepath.Join(targetPath, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > maxManifestBytes {
			data = data[:maxManifestBytes]
		}
		sb.WriteString(fmt.Sprintf("\n### %s\n```\n%s\n```\n", name, string(data)))
	}
	return sb.String()
}

func collectRepoXray(targetPath string) string {
	tree := buildTree(targetPath, 0, 3)
	manifests := collectManifests(targetPath)
	return fmt.Sprintf("## Directory Structure\n```\n%s```\n## Key Manifest Files\n%s", tree, manifests)
}

// ---------------------------------------------------------------------------
// Phase 2: Blueprint Generation
// ---------------------------------------------------------------------------

// generateBlueprintAI runs OpenCode interactively so the user can see its
// thinking in real time. The prompt instructs it to write the result directly
// to .harness/blueprint.md. The call is bounded by a timeout and we measure
// elapsed time so the user knows how much the AI consumed.
func generateBlueprintAI(model, contextData, harnessDir string) error {
	bootstrapperRaw, err := TemplatesFS.ReadFile("templates/bootstrapper.md")
	if err != nil {
		return fmt.Errorf("could not read embedded bootstrapper template: %w", err)
	}

	blueprintPath := filepath.Join(harnessDir, "blueprint.md")
	timeout := time.Duration(config.DefaultTimeoutSec) * time.Second

	// Inject the output path and timing context into the prompt.
	prompt := string(bootstrapperRaw) +
		fmt.Sprintf("\n\nIMPORTANT: Write your output DIRECTLY to the file `%s`. Do not print it to the terminal.\n\n", blueprintPath) +
		contextData

	fmt.Printf("\n🤖 Starting AI blueprint generation (timeout: %s)\n", timeout)
	fmt.Println("────────────────────────────────────────────────────────────────────────")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "opencode", "run",
		"--model", model,
		"--dangerously-skip-permissions",
		"--thinking",
		prompt,
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)

	fmt.Println("────────────────────────────────────────────────────────────────────────")
	fmt.Printf("⏱  Thinking time: %s\n", elapsed.Round(time.Millisecond))

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("❌ AI timed out after %s (limit: %s) — try increasing 'timeout' in agents.yml or switching model",
			elapsed.Round(time.Millisecond), timeout)
	}
	if runErr != nil {
		return fmt.Errorf("opencode exited with error after %s: %w", elapsed.Round(time.Millisecond), runErr)
	}

	// ── Validation ────────────────────────────────────────────────────────────
	const minBytes = 100 // a real blueprint is always larger than this

	info, statErr := os.Stat(blueprintPath)
	if statErr != nil {
		return fmt.Errorf(
			"❌ Validation failed: .harness/blueprint.md was not created by the AI.\n"+
				"   Try running init again or switch to Manual mode.\n"+
				"   (underlying error: %w)", statErr)
	}
	if info.Size() < minBytes {
		content, _ := os.ReadFile(blueprintPath)
		return fmt.Errorf(
			"❌ Validation failed: .harness/blueprint.md is empty or incomplete (%d bytes).\n"+
				"   Content found:\n---\n%s\n---\n"+
				"   Try running init again or switch to Manual mode.",
			info.Size(), string(content))
	}

	fmt.Printf("✅ blueprint.md validated — %d bytes written in %s\n", info.Size(), elapsed.Round(time.Millisecond))
	return nil
}

// generateBlueprintManual fills the blueprint template literally with interview data.
func generateBlueprintManual(contextData, harnessDir string) error {
	// Parse lines from the context block (Key: Value format)
	values := map[string]string{}
	for _, line := range strings.Split(contextData, "\n") {
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) == 2 {
			values[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	blueprintMD := fmt.Sprintf(`# Project Manifest (Blueprint)

## 1. Base Technology Stack
- **Name**: %s
- **Language/Stack**: %s
- **Dependency Manager**: %s

## 2. Architecture & Design Patterns
- %s

## 3. UI/UX & Design System
- %s

## 4. Security & Best Practices
- %s

## 5. Testing Strategy
- Define your test command and coverage targets here.

## 6. Local Validation Pipeline (Commands)
- Test command: # add here
- Lint command: # add here
- Build command: # add here
`,
		values["Name"], values["Stack"], values["Dependency Manager"],
		values["Architecture"], orNA(values["UI/UX"]), values["Security"],
	)

	blueprintPath := filepath.Join(harnessDir, "blueprint.md")
	if err := os.WriteFile(blueprintPath, []byte(blueprintMD), 0644); err != nil {
		return fmt.Errorf("could not write blueprint.md: %w", err)
	}
	return nil
}

func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}

// ---------------------------------------------------------------------------
// Scaffold helpers
// ---------------------------------------------------------------------------

func writeAgentsYAML(harnessDir, model string) error {
	agentsYAML := fmt.Sprintf(`global_settings:
  # Maximum seconds to wait for any single OpenCode invocation.
  timeout: 300
  # Thinking budget in milliseconds (0 = no explicit limit, model decides).
  thinking_budget_ms: 0
agents:
  architect:
    model: "%s"
  builder:
    model: "%s"
  gatekeeper:
    model: "%s"
`, model, model, model)
	return os.WriteFile(filepath.Join(harnessDir, "agents.yml"), []byte(agentsYAML), 0644)
}

func writeTasksJSON(harnessDir, projName string) error {
	type task struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	type projectState struct {
		Project string `json:"project"`
		Tasks   []task `json:"tasks"`
	}
	data, err := json.MarshalIndent(projectState{
		Project: projName,
		Tasks: []task{{
			ID:     "T001",
			Title:  "Initialize the development harness and validate the environment",
			Status: "pending",
		}},
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(harnessDir, "state", "tasks.json"), data, 0644)
}

// ---------------------------------------------------------------------------
// Public Entry Point
// ---------------------------------------------------------------------------

// InitProject orchestrates the full interactive bootstrapper flow.
func InitProject(targetPath string) error {
	reader := bufio.NewReader(os.Stdin)
	harnessDir := filepath.Join(targetPath, ".harness")

	// ── Create folder skeleton ──────────────────────────────────────────────
	for _, d := range []string{
		filepath.Join(harnessDir, "state"),
		filepath.Join(harnessDir, "specs"),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("error creating folder %s: %w", d, err)
		}
	}

	// ── Phase 0: Model selection ────────────────────────────────────────────
	fmt.Println("\n🔍 Fetching available models from opencode...")
	models, err := fetchAvailableModels()
	if err != nil || len(models) == 0 {
		fmt.Println("⚠️  Could not retrieve model list. Falling back to default model.")
		models = []string{"google/gemini-2.5-flash"}
	}

	selectedModel, err := selectModel(reader, models)
	if err != nil {
		return fmt.Errorf("model selection failed: %w", err)
	}
	fmt.Printf("✔  Selected model: %s\n", selectedModel)

	aiMode, err := askBlueprintMode(reader)
	if err != nil {
		return fmt.Errorf("mode selection failed: %w", err)
	}

	// ── Phase 1: Context collection ─────────────────────────────────────────
	blank, err := isBlankProject(targetPath)
	if err != nil {
		return fmt.Errorf("could not scan project directory: %w", err)
	}

	var contextData string

	if blank {
		// Scenario A: blank canvas → always questionnaire
		fmt.Println("\n✨ Blank project detected. Let's fill in the details.")
		contextData, err = runQuestionnaire(reader)
		if err != nil {
			return fmt.Errorf("questionnaire error: %w", err)
		}
	} else {
		// Scenario B: existing project → ask preference
		fmt.Println("\n⚠️  This directory already contains project files.")
		fmt.Println("   [1] Manual questionnaire")
		fmt.Println("   [2] AI architecture scan (uses more tokens)")
		fmt.Print("\nChoose [1/2]: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		if choice == "2" && aiMode {
			fmt.Println("🔬 Scanning repository structure...")
			contextData = collectRepoXray(targetPath)
		} else {
			contextData, err = runQuestionnaire(reader)
			if err != nil {
				return fmt.Errorf("questionnaire error: %w", err)
			}
		}
	}

	// ── Phase 2: Blueprint generation ──────────────────────────────────────
	if aiMode {
		if err := generateBlueprintAI(selectedModel, contextData, harnessDir); err != nil {
			return fmt.Errorf("AI blueprint generation failed: %w", err)
		}
	} else {
		if err := generateBlueprintManual(contextData, harnessDir); err != nil {
			return fmt.Errorf("manual blueprint generation failed: %w", err)
		}
	}

	// ── Scaffold: agents.yml and tasks.json ─────────────────────────────────
	projName := filepath.Base(func() string { abs, _ := filepath.Abs(targetPath); return abs }())

	if err := writeAgentsYAML(harnessDir, selectedModel); err != nil {
		return fmt.Errorf("could not write agents.yml: %w", err)
	}
	if err := writeTasksJSON(harnessDir, projName); err != nil {
		return fmt.Errorf("could not write tasks.json: %w", err)
	}

	// ── Final message (exact wording as specified) ──────────────────────────
	fmt.Println("\nWe have generated the file .harness/blueprint.md, when the configuration process finishes, give it a read to ensure the technology stack is correct, or simply complement what you think is necessary.")
	return nil
}