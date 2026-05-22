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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"harness-cli/internal/config"
	"harness-cli/internal/ui"
)

//go:embed templates/*.md
var TemplatesFS embed.FS

// getTheme returns a customized huh theme matching the Coco Clean aesthetic
func getTheme() *huh.Theme {
	t := huh.ThemeCharm()
	// Adjust focused styles to use Primary (Deep Purple) and Text
	t.Focused.Title = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary)).Bold(true)
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary)).Bold(true)
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Text))
	t.Focused.FocusedButton = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color(ui.Primary)).Bold(true).Padding(0, 1)
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary))
	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary))
	return t
}

// ---------------------------------------------------------------------------
// Dependency Check
// ---------------------------------------------------------------------------

// CheckOpenCode verifies that the `opencode` binary is available in $PATH.
func CheckOpenCode() error {
	_, err := exec.LookPath("opencode")
	if err != nil {
		ui.PrintError("'opencode' is not installed or not found in your $PATH.\n   It is a mandatory dependency for smidhus-harness.\n   Install it and try again: https://opencode.ai")
		os.Exit(1)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Phase 0: Model Selection
// ---------------------------------------------------------------------------

// fetchAvailableModels runs `opencode models` silently and returns mapped providers to models.
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
			modelName = line // Keeping full string like google/gemini...
		} else {
			provider = "other"
			modelName = line
		}
		modelMap[provider] = append(modelMap[provider], modelName)
	}
	return modelMap, nil
}

// selectModel presents a 2-step sequential flow for principal model selection.
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

// askBlueprintMode asks whether to use AI or manual mode for blueprint generation.
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
// Phase 1: Project Scanning
// ---------------------------------------------------------------------------

var noiseFiles = map[string]bool{
	".git": true, ".DS_Store": true, ".gitignore": true,
	".idea": true, ".vscode": true, "README.md": true, "readme.md": true,
	".harness": true, "node_modules": true,
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

// ---------------------------------------------------------------------------
// Phase 1B: AI Scan (advanced project, AI mode)
// ---------------------------------------------------------------------------

var ignoreForTree = map[string]bool{
	"node_modules": true, ".git": true, "bin": true, "dist": true,
	"build": true, ".harness": true, "__pycache__": true, ".next": true,
	"vendor": true, "target": true,
}

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

// opencodeEvent is a minimal representation of a JSON event emitted by
// `opencode run --format json`. We only decode the fields we care about.
type opencodeEvent struct {
	Type string `json:"type"`
	Part struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		Title string `json:"title"`
		// tool_call fields
		Tool  string `json:"tool"`
		State string `json:"state"`
		Input any    `json:"input"`
	} `json:"part"`
	Error struct {
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	} `json:"error"`
}

type parsedErrorInner struct {
	Name       string `json:"name"`
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
	Type       string `json:"type"`
	Data       struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	} `json:"data"`
}

type parsedErrorLog struct {
	Error            *parsedErrorInner `json:"error"`
	parsedErrorInner                   // embedded
}

// formatErrorLine formats very long error lines to extract and highlight the key details.
func formatErrorLine(line string) string {
	idx := strings.Index(line, "error={")
	if idx == -1 {
		if len(line) > 500 {
			return line[:497] + "..."
		}
		return line
	}

	metadata := line[:idx]
	errPart := line[idx+6:]

	var parsed parsedErrorLog
	if json.Unmarshal([]byte(errPart), &parsed) == nil {
		inner := &parsed.parsedErrorInner
		if parsed.Error != nil {
			inner = parsed.Error
		}

		errMsg := inner.Message
		if inner.Data.Error.Message != "" {
			errMsg = inner.Data.Error.Message
		}
		errType := inner.Type
		if inner.Data.Error.Type != "" {
			errType = inner.Data.Error.Type
		}

		return fmt.Sprintf("%serror={Name: %s, StatusCode: %d, Type: %s, Message: %s}",
			metadata, inner.Name, inner.StatusCode, errType, errMsg)
	}

	if len(line) > 500 {
		return line[:497] + "..."
	}
	return line
}

// classifyError analyzes a log line (usually from stderr under --print-logs)
// and returns the classified error message and true if it is a fatal API error.
func classifyError(line string) (string, bool) {
	lower := strings.ToLower(line)

	isErrLog := strings.Contains(line, "ERROR") || 
		strings.Contains(line, "error=") || 
		strings.Contains(lower, "error:") || 
		strings.Contains(lower, "[error]") || 
		strings.Contains(lower, `"level":50`) || 
		strings.Contains(lower, `level:50`) || 
		strings.Contains(lower, `"level":"error"`) || 
		strings.Contains(lower, `"level":"fatal"`) || 
		strings.Contains(lower, "unknownerror") ||
		strings.Contains(lower, "apierror")

	if !isErrLog {
		return "", false
	}

	// Rate limit / Quota errors
	if strings.Contains(lower, "usage_limit_reached") || 
		strings.Contains(lower, "rate_limit_exceeded") || 
		strings.Contains(lower, "rate_limit") || 
		strings.Contains(lower, "quota_exceeded") || 
		strings.Contains(lower, "usage limit reached") || 
		strings.Contains(lower, "rate limit exceeded") {
		return "API Quota/Rate Limit Exceeded. Process aborted to prevent hang.", true
	}

	// Unexpected server error / UnknownError
	if strings.Contains(lower, "unexpected server error") || 
		strings.Contains(lower, "unknownerror") || 
		strings.Contains(lower, "unknown error") {
		return "Unexpected server error. Process aborted.", true
	}

	// Context window limit
	if strings.Contains(lower, "context_length_exceeded") || 
		strings.Contains(lower, "context length") || 
		strings.Contains(lower, "max_tokens") {
		return "Context window limit exceeded. Process aborted.", true
	}

	// AI Call errors general fallback
	if strings.Contains(lower, "ai_apicallerror") || 
		strings.Contains(lower, "api_apicallerror") || 
		strings.Contains(lower, "apierror") || 
		strings.Contains(lower, "api_error") {
		
		// Try to parse the inner message from the line.
		if idx := strings.Index(lower, "message\":\""); idx != -1 {
			msgStart := idx + 10
			if endIdx := strings.Index(lower[msgStart:], "\""); endIdx != -1 {
				msg := line[msgStart : msgStart+endIdx]
				if msg != "" {
					return fmt.Sprintf("API Call Error: %s", msg), true
				}
			}
		}
		return "API Call Error. Process aborted to prevent hang.", true
	}

	// Fallback error classification
	return "API Call Error. Process aborted to prevent hang.", true
}

func runOpencodeWithFilter(ctx context.Context, model, prompt string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, "opencode", "run",
		"--model", model,
		"--dangerously-skip-permissions",
		"--thinking",
		"--format", "json",
		"--print-logs",
		prompt,
	)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return "", err
	}

	// Styles for rendering thinking blocks and tool calls in the terminal
	thinkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.Primary)).
		Italic(true)
	thinkHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.Primary)).
		Bold(true)
	toolStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.Muted))

	var (
		mu                sync.Mutex
		killedReason      string
		rateLimitDetected bool
		detectedError     string
		stderrBuf         bytes.Buffer
		stderrMu          sync.Mutex
	)

	killProcess := func(reason string) {
		mu.Lock()
		if killedReason == "" {
			killedReason = reason
		}
		mu.Unlock()
		cancel()
	}

	var lastActivity int64
	atomic.StoreInt64(&lastActivity, time.Now().UnixNano())

	updateActivity := func() {
		atomic.StoreInt64(&lastActivity, time.Now().UnixNano())
	}

	var stdoutBuf bytes.Buffer
	var wg sync.WaitGroup

	// Monitor stdout: Keep existing logic intact but use bufio.Reader to prevent limits
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stdoutPipe)
		inThinking := false

		for {
			raw, err := reader.ReadString('\n')
			if len(raw) > 0 {
				updateActivity()
				stdoutBuf.WriteString(raw)

				var evt opencodeEvent
				if json.Unmarshal([]byte(raw), &evt) == nil {
					switch evt.Type {
					case "assistant":
						// Model/session header — not shown (internal event)

					case "step_start":
						// New step beginning — close thinking block if open
						if inThinking {
							fmt.Println(thinkHeaderStyle.Render("└─ end thinking"))
							inThinking = false
						}

					case "reasoning":
						// The AI's reasoning/thinking block
						if !inThinking {
							fmt.Println(thinkHeaderStyle.Render("┌─ Thinking..."))
							inThinking = true
						}
						if evt.Part.Text != "" {
							for _, line := range strings.Split(evt.Part.Text, "\n") {
								fmt.Println(thinkStyle.Render("│ " + line))
							}
						}

					case "tool_call":
						// Tool invocation (read file, patch, etc.)
						if inThinking {
							fmt.Println(thinkHeaderStyle.Render("└─ end thinking"))
							inThinking = false
						}
						if evt.Part.State == "pending" {
							fmt.Println(toolStyle.Render("→ " + evt.Part.Tool))
						}

					case "text":
						// The model's final text response — we do NOT print it
						if inThinking {
							fmt.Println(thinkHeaderStyle.Render("└─ end thinking"))
							inThinking = false
						}

					case "step_finish":
						if inThinking {
							fmt.Println(thinkHeaderStyle.Render("└─ end thinking"))
							inThinking = false
						}

					case "error":
						fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Error)).Render(
							"✗ AI error: " + evt.Error.Data.Message,
						))
						mu.Lock()
						detectedError = evt.Error.Data.Message
						mu.Unlock()
						killProcess("api_error")
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Monitor stderr concurrently (with StderrPipe) for fail-fast logs
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stderrPipe)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				updateActivity()

				stderrMu.Lock()
				stderrBuf.WriteString(line)
				stderrMu.Unlock()

				if errMsg, ok := classifyError(line); ok {
					mu.Lock()
					detectedError = errMsg
					if strings.Contains(errMsg, "Quota/Rate Limit Exceeded") {
						rateLimitDetected = true
					}
					mu.Unlock()
					killProcess("api_error")
					return
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Watchdog goroutine: Poll every 2 seconds to check for inactivity
	watchdogDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchdogDone:
				return
			case <-ticker.C:
				last := atomic.LoadInt64(&lastActivity)
				if time.Since(time.Unix(0, last)) > 2*time.Minute {
					killProcess("inactivity")
					return
				}
			}
		}
	}()

	runErr := cmd.Wait()
	close(watchdogDone)
	wg.Wait() // Wait for stdout and stderr scanners to finish draining

	mu.Lock()
	reason := killedReason
	isRateLimit := rateLimitDetected
	apiErr := detectedError
	mu.Unlock()

	// ── Rate Limit Error ───────────────────────────────────────────────────────
	if isRateLimit {
		return stdoutBuf.String(), fmt.Errorf("API Quota/Rate Limit Exceeded. Process aborted to prevent hang.")
	}

	// ── Classified API Error ───────────────────────────────────────────────────
	if apiErr != "" {
		return stdoutBuf.String(), fmt.Errorf("%s", apiErr)
	}

	// ── Inactivity Timeout ─────────────────────────────────────────────────────
	if reason == "inactivity" {
		return stdoutBuf.String(), fmt.Errorf(
			"Inactivity timeout: No response/output received from the model for 2 minutes.\n" +
				"  • The API might be overloaded or the task is too complex.\n" +
				"  • Switch to a faster model or try again later.",
		)
	}

	if runErr != nil {
		stderrMu.Lock()
		stderrLines := strings.TrimSpace(stderrBuf.String())
		stderrMu.Unlock()

		if stderrLines != "" {
			lines := strings.Split(stderrLines, "\n")
			var lastLine string
			for i := len(lines) - 1; i >= 0; i-- {
				trimmed := strings.TrimSpace(lines[i])
				if trimmed != "" {
					lastLine = trimmed
					break
				}
			}
			if lastLine != "" {
				return stdoutBuf.String(), fmt.Errorf("failed after execution: %s", lastLine)
			}
		}
		return stdoutBuf.String(), runErr
	}

	return stdoutBuf.String(), nil
}

func generateBlueprintAI(model, contextData, harnessDir string) error {
	bootstrapperRaw, err := TemplatesFS.ReadFile("templates/bootstrapper.md")
	if err != nil {
		return fmt.Errorf("could not read embedded bootstrapper template: %w", err)
	}

	blueprintPath := filepath.Join(harnessDir, "blueprint.md")
	timeout := time.Duration(config.DefaultTimeoutSec) * time.Second

	prompt := string(bootstrapperRaw) +
		fmt.Sprintf("\n\nIMPORTANT: Write your output DIRECTLY to the file `%s`. Do not print it to the terminal.\n\n", blueprintPath) +
		contextData

	ui.PrintKeyValue("Action", "Starting AI blueprint generation...")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	rawOutput, runErr := runOpencodeWithFilter(ctx, model, prompt)
	elapsed := time.Since(start)

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("AI timed out after %s (limit: %s) — try increasing 'timeout' in agents.yml or switching model",
			elapsed.Round(time.Millisecond), timeout)
	}
	if runErr != nil {
		return fmt.Errorf("opencode exited with error after %s: %w", elapsed.Round(time.Millisecond), runErr)
	}

	// Extract markdown from stdout as fallback if file wasn't created properly
	const minBytes = 100
	info, statErr := os.Stat(blueprintPath)
	if statErr != nil || info.Size() < minBytes {
		mdStart := strings.Index(rawOutput, "# ")
		if mdStart != -1 {
			blueprintContent := rawOutput[mdStart:]
			os.WriteFile(blueprintPath, []byte(blueprintContent), 0644)
		}
	}

	// ── Validation ────────────────────────────────────────────────────────────
	info, statErr = os.Stat(blueprintPath)
	if statErr != nil {
		return fmt.Errorf("Validation failed: .harness/blueprint.md was not created by the AI")
	}
	if info.Size() < minBytes {
		return fmt.Errorf("Validation failed: .harness/blueprint.md is empty or incomplete (%d bytes)", info.Size())
	}

	ui.PrintSuccess(fmt.Sprintf(".harness/blueprint.md generated — %d bytes written in %s", info.Size(), elapsed.Round(time.Millisecond)))
	return nil
}

func generateBlueprintManual(contextData, harnessDir string) error {
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

func generateAgentsAI(model string, budget int, modelMap map[string][]string, harnessDir string) error {
	agentsYAMLPath := filepath.Join(harnessDir, "agents.yml")
	timeout := time.Duration(config.DefaultTimeoutSec) * time.Second

	// Flatten model map to a simple list for the prompt
	var allModels []string
	for _, models := range modelMap {
		allModels = append(allModels, models...)
	}
	availableModelsStr := strings.Join(allModels, "\n- ")

	prompt := fmt.Sprintf(`You are an AI architect configuring a multi-agent system.
Based on the following list of available AI models, fill out the agents.yml template.
Assign high-reasoning models to complex agents (architect, builder, gatekeeper, cloud) and fast/standard models to simple agents (designer, documenter, maestre).
You must use ONLY models from this list:
- %s

IMPORTANT: Write your output DIRECTLY to the file '%s' using the exact structure below. Do not print anything else.

# =======================================================================
# SMIDHUS HARNESS - AGENTS CONFIGURATION
# =======================================================================
# You can edit this file at any time to change the models assigned to
# each agent. If an agent is struggling with a complex task, try upgrading
# its model to one with higher reasoning capabilities.

global_settings:
  # Maximum seconds to wait for any single OpenCode invocation.
  timeout: 900
  # Thinking budget in milliseconds (0 = no explicit limit, model decides).
  thinking_budget_ms: %d

agents:
  # ARCHITECT: Analyzes requirements, designs the system, and dictates technical rules.
  # Recommended: High Reasoning Models (e.g., nvidia/qwen3-next-80b-a3b-thinking, deepseek-v4-pro)
  architect:
    model: "[Select Model]"

  # BUILDER: Writes the core business logic, components, and implements the specs.
  # Recommended: High Reasoning Models (e.g., nvidia/qwen3-next-80b-a3b-thinking, deepseek-v4-pro)
  builder:
    model: "[Select Model]"

  # GATEKEEPER: Reviews code, enforces security, and ensures blueprint compliance.
  # Recommended: High Reasoning Models
  gatekeeper:
    model: "[Select Model]"

  # CLOUD: Manages IaC, deployment scripts, and infrastructure configuration.
  # Recommended: High Reasoning Models
  cloud:
    model: "[Select Model]"

  # DESIGNER: Handles UI/UX components, CSS, and aesthetic implementations.
  # Recommended: Fast/Standard Models (e.g., google/gemini-2.5-flash, qwen3.6-plus)
  designer:
    model: "[Select Model]"

  # DOCUMENTER: Generates READMEs, inline comments, and API documentation.
  # Recommended: Fast/Standard Models
  documenter:
    model: "[Select Model]"

  # MAESTRE: Orchestrates tasks, updates the state machine, and plans the workflow.
  # Recommended: Fast/Standard Models
  maestre:
    model: "[Select Model]"
`, availableModelsStr, agentsYAMLPath, budget)

	ui.PrintKeyValue("Action", "Generating agents.yml using AI...")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	rawOutput, runErr := runOpencodeWithFilter(ctx, model, prompt)
	elapsed := time.Since(start)

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("AI timed out after %s", elapsed.Round(time.Millisecond))
	}
	if runErr != nil {
		return fmt.Errorf("opencode exited with error after %s: %w", elapsed.Round(time.Millisecond), runErr)
	}

	// Extract YAML from stdout as fallback if file wasn't created
	info, statErr := os.Stat(agentsYAMLPath)
	if statErr != nil || info.Size() < 50 {
		yamlStart := strings.Index(rawOutput, "# =======================================================================")
		if yamlStart != -1 {
			yamlContent := rawOutput[yamlStart:]
			// Try to strip anything after the YAML if there's conversational trailing text
			endIdx := strings.LastIndex(yamlContent, "model: \"")
			if endIdx != -1 {
				// Just blindly write the block assuming the AI formatted it properly
				os.WriteFile(agentsYAMLPath, []byte(yamlContent), 0644)
			}
		} else if strings.Contains(rawOutput, "global_settings:") {
			// Fallback if the comment block was omitted
			yamlStart = strings.Index(rawOutput, "global_settings:")
			if yamlStart != -1 {
				os.WriteFile(agentsYAMLPath, []byte(rawOutput[yamlStart:]), 0644)
			}
		}
	}

	ui.PrintSuccess(fmt.Sprintf(".harness/agents.yml generated in %s", elapsed.Round(time.Millisecond)))
	return nil
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
			Title:  "Scaffold the base project structure and configure core dependencies according to the Blueprint",
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

func InitProject(targetPath string) error {
	ui.PrintBanner()

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

	// ── Create specs templates ──────────────────────────────────────────────
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

	// ── Phase 0: Model selection ────────────────────────────────────────────
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

	// ── Phase 1: Context collection ─────────────────────────────────────────
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

	// ── Final Configuration (Agents YAML) ───────────────────────────────────
	if err := generateAgentsAI(selectedModel, budget, modelMap, harnessDir); err != nil {
		return fmt.Errorf("could not generate agents.yml: %w", err)
	}
	if err := writeTasksJSON(harnessDir, defaultName); err != nil {
		return fmt.Errorf("could not write tasks.json: %w", err)
	}

	// ── Final message (exact wording as specified) ──────────────────────────
	finalMsg := "We have generated the file .harness/blueprint.md, when the configuration process finishes, give it a read to ensure the technology stack is correct, or simply complement what you think is necessary."
	fmt.Println("\n" + ui.RenderMessage(finalMsg))
	return nil
}