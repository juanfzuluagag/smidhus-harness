package setup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"harness-cli/internal/config"
	"harness-cli/internal/ui"
)

// ---------------------------------------------------------------------------
// runOpencodeWithFilter executes `opencode run` with structured JSON output,
// streaming thinking blocks and tool calls to the terminal in real time.
// It implements fail-fast on API/quota/rate-limit errors via the stderr watchdog.
// Using a cancellable context (not a timeout) means the watchdog is the sole
// authority for inactivity termination, keeping the logic in one place.
func runOpencodeWithFilter(ctx context.Context, model, prompt string, p *tea.Program) (string, error) {
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

	// ── Spinner for Initial Wait (only in non-TUI mode) ──────────────────────
	var stopSpinner func() = func() {}
	if p == nil {
		spinnerChan := make(chan struct{})
		go func() {
			frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
			i := 0
			ticker := time.NewTicker(80 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-spinnerChan:
					fmt.Print("\r\x1b[K") // Clear the line when stopped
					return
				case <-ticker.C:
					frame := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary)).Render(frames[i%len(frames)])
					fmt.Printf("\r %s Waiting for AI response...", frame)
					i++
				}
			}
		}()

		var spinnerOnce sync.Once
		stopSpinner = func() {
			spinnerOnce.Do(func() {
				close(spinnerChan)
				time.Sleep(20 * time.Millisecond) // Ensure the goroutine finishes clearing the line
			})
		}
	}
	defer stopSpinner()

	if err := cmd.Start(); err != nil {
		return "", err
	}

	// Styles for rendering thinking blocks and tool calls in the terminal
	thinkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#737373")).
		Italic(true)
	thinkHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#737373")).
		Bold(true)
	toolStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.Muted))

	printLog := func(msg string) {
		if p != nil {
			p.Send(ui.LogMsg(msg + "\n"))
		} else {
			fmt.Println(msg)
		}
	}

	printThinking := func(msg string) {
		if p != nil {
			p.Send(ui.ThinkingMsg(msg + "\n"))
		} else {
			fmt.Println(msg)
		}
	}

	var (
		mu                sync.Mutex
		killedReason      string
		rateLimitDetected bool
		detectedError     string
		stderrBuf         bytes.Buffer
		stderrMu          sync.Mutex
	)

	// killProcess is idempotent: first caller wins, subsequent calls are no-ops.
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

	// Stream stdout events: thinking blocks and tool calls are rendered in real
	// time so the user can follow along. Raw output is accumulated for fallback
	// extraction if the AI writes its response to stdout instead of a file.
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stdoutPipe)
		inThinking := false

		for {
			raw, err := reader.ReadString('\n')
			if len(raw) > 0 {
				stopSpinner()
				updateActivity()
				stdoutBuf.WriteString(raw)

				var evt opencodeEvent
				if json.Unmarshal([]byte(raw), &evt) == nil {
					eventType := evt.Type
					if eventType == "message.part.updated" || eventType == "message_part_updated" {
						if evt.Part.Type != "" {
							if evt.Part.Type == "tool" {
								eventType = "tool_call"
							} else {
								eventType = evt.Part.Type
							}
						}
					}

					switch eventType {
					case "assistant":
						// Model/session header — not shown (internal event)

					case "step_start":
						// New step beginning — close thinking block if open
						if inThinking {
							printThinking(thinkHeaderStyle.Render("└─ end thinking"))
							inThinking = false
						}

					case "reasoning":
						// The AI's reasoning/thinking block
						if !inThinking {
							printThinking(thinkHeaderStyle.Render("┌─ Thinking..."))
							inThinking = true
						}
						if evt.Part.Text != "" {
							for _, line := range strings.Split(evt.Part.Text, "\n") {
								printThinking(thinkStyle.Render("│ " + line))
							}
						}

					case "tool_call":
						// Tool invocation (read file, patch, etc.)
						if inThinking {
							printThinking(thinkHeaderStyle.Render("└─ end thinking"))
							inThinking = false
						}
						if evt.Part.State == "pending" {
							printThinking(toolStyle.Render("→ " + evt.Part.Tool))
						}

					case "text":
						// The model's final text response — we do NOT print it
						if inThinking {
							printThinking(thinkHeaderStyle.Render("└─ end thinking"))
							inThinking = false
						}

					case "step_finish":
						if inThinking {
							printThinking(thinkHeaderStyle.Render("└─ end thinking"))
							inThinking = false
						}

					case "error":
						printLog(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Error)).Render(
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

	// Monitor stderr for fail-fast error logs emitted by --print-logs.
	// A dedicated goroutine avoids blocking stdout consumption, which would
	// deadlock the process if the pipe buffers fill up.
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stderrPipe)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				stopSpinner()
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

	// Watchdog: cancel the process if the model goes silent for 10 minutes.
	// Ten minutes is chosen to be long enough for slow reasoning models but
	// short enough to catch a hung or rate-limited connection.
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
				if time.Since(time.Unix(0, last)) > 10*time.Minute {
					killProcess("inactivity")
					return
				}
			}
		}
	}()

	runErr := cmd.Wait()
	close(watchdogDone)
	wg.Wait() // drain both pipe goroutines before inspecting results

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

// ---------------------------------------------------------------------------
// generateBlueprintAI sends the bootstrapper prompt plus collected context to
// opencode. The AI writes directly to blueprint.md; stdout is a fallback for
// models that print output instead of saving files.
func generateBlueprintAI(model, contextData, harnessDir string, p *tea.Program) error {
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
	rawOutput, runErr := runOpencodeWithFilter(ctx, model, prompt, p)
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

// generateBlueprintManual produces a simple markdown scaffold from the
// questionnaire answers when the user opts out of AI mode.
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
// agentsPromptData holds the template variables interpolated into prompt_agents.txt.
type agentsPromptData struct {
	Models         string
	AgentsYAMLPath string
	Budget         int
}

// generateAgentsAI asks the AI to fill in the agents.yml template by choosing
// appropriate models from the available list. Using text/template keeps the
// long prompt out of the Go source and makes it independently editable.
func generateAgentsAI(model string, budget int, modelMap map[string][]string, harnessDir string, p *tea.Program) error {
	agentsYAMLPath := filepath.Join(harnessDir, "agents.yml")
	timeout := time.Duration(config.DefaultTimeoutSec) * time.Second

	// Flatten the provider->models map into a single list for the prompt.
	var allModels []string
	for _, models := range modelMap {
		allModels = append(allModels, models...)
	}

	// Read and render the agents prompt template
	promptTmplRaw, err := TemplatesFS.ReadFile("templates/prompt_agents.txt")
	if err != nil {
		return fmt.Errorf("could not read embedded prompt_agents template: %w", err)
	}
	tmpl, err := template.New("agents_prompt").Parse(string(promptTmplRaw))
	if err != nil {
		return fmt.Errorf("could not parse prompt_agents template: %w", err)
	}
	var promptBuf strings.Builder
	if err := tmpl.Execute(&promptBuf, agentsPromptData{
		Models:         strings.Join(allModels, "\n- "),
		AgentsYAMLPath: agentsYAMLPath,
		Budget:         budget,
	}); err != nil {
		return fmt.Errorf("could not render prompt_agents template: %w", err)
	}

	ui.PrintKeyValue("Action", "Generating agents.yml using AI...")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	rawOutput, runErr := runOpencodeWithFilter(ctx, model, promptBuf.String(), p)
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
			endIdx := strings.LastIndex(yamlContent, "model: \"")
			if endIdx != -1 {
				os.WriteFile(agentsYAMLPath, []byte(yamlContent), 0644)
			}
		} else if strings.Contains(rawOutput, "global_settings:") {
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
