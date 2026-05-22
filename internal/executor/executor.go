package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"harness-cli/internal/config"
	"harness-cli/internal/skills"
	"harness-cli/internal/ui"
)

// knownErrorPatterns match actionable error messages in opencode's text output.
// These appear in either stderr (formatted text) or stdout (JSON events).
var knownErrorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)usage.?limit.*(reached|exceeded|has been)`),
	regexp.MustCompile(`(?i)rate.?limit`),
	regexp.MustCompile(`(?i)quota.?exceeded`),
	regexp.MustCompile(`(?i)context.?(length|window|limit)`),
	regexp.MustCompile(`(?i)authentication|unauthorized|invalid.*(api.?key|token)`),
	regexp.MustCompile(`(?i)model.*not found|no such model`),
	regexp.MustCompile(`(?i)retrying in \d+`),
	regexp.MustCompile(`(?i)UnknownError|unknown error`),
	regexp.MustCompile(`(?i)unexpected server error`),
	regexp.MustCompile(`(?i)APIError|api error`),
	regexp.MustCompile(`(?i)server.?error|internal error`),
	regexp.MustCompile(`(?i)check server logs`),
	regexp.MustCompile(`(?i)capacity|overloaded|service unavailable`),
}

// opencodeEvent represents a JSON event emitted by opencode run.
type opencodeEvent struct {
	Type string `json:"type"`
	Part struct {
		Text  string `json:"text"`
		Tool  string `json:"tool"`
		State string `json:"state"`
	} `json:"part"`
	Error struct {
		Name string `json:"name"`
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

// RunAgent invokes OpenCode with the correct model and agent prompt.
// It monitors both stdout (for JSON thinking events) and stderr (for text errors and logs)
// in real time, implementing a fail-fast mechanism on API/Quota/Rate Limit errors.
// Returns elapsed time on success.
func RunAgent(agentName string, cfg config.AgentConfig, agentContent string, timeout time.Duration) (time.Duration, error) {
	if len(cfg.Skills) > 0 {
		ui.PrintInfo(fmt.Sprintf("[%s] Equipping skills: %v", agentName, cfg.Skills))
		if err := skills.AutoEquip(cfg.Skills); err != nil {
			return 0, err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "opencode", "run",
		"--model", cfg.Model,
		"--dangerously-skip-permissions",
		"--thinking",
		"--format", "json",
		"--print-logs",
		agentContent,
	)

	// Pipe both stdout and stderr so we can monitor both streams.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("could not attach stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return 0, fmt.Errorf("could not attach stderr pipe: %w", err)
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("could not start opencode for agent %s: %w", agentName, err)
	}

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
		rateLimitDetected bool
		detectedError     string
		detectedLines     []string
		stderrBuf         bytes.Buffer
		stderrMu          sync.Mutex
	)

	addError := func(msg string) {
		clean := strings.TrimSpace(stripAnsiCodes(msg))
		if clean == "" {
			return
		}
		mu.Lock()
		detectedLines = append(detectedLines, clean)
		mu.Unlock()
	}

	var lastActivity int64
	atomic.StoreInt64(&lastActivity, time.Now().UnixNano())

	updateActivity := func() {
		atomic.StoreInt64(&lastActivity, time.Now().UnixNano())
	}

	var wg sync.WaitGroup

	// Monitor stdout — opencode emits JSON event objects with thinking and tool calls under --format json.
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stdoutPipe)
		inThinking := false
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				updateActivity()

				// Try to parse as a JSON event (opencode --format json style).
				var evt opencodeEvent
				if json.Unmarshal([]byte(line), &evt) == nil {
					switch evt.Type {
					case "assistant":
						// Model/session header — not shown

					case "step_start":
						if inThinking {
							fmt.Println(thinkHeaderStyle.Render("└─ end thinking"))
							inThinking = false
						}

					case "reasoning":
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
						if inThinking {
							fmt.Println(thinkHeaderStyle.Render("└─ end thinking"))
							inThinking = false
						}
						if evt.Part.State == "pending" {
							fmt.Println(toolStyle.Render("→ " + evt.Part.Tool))
						}

					case "text":
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
						cancel()
						return
					}
				}

				// Also match plain text patterns in stdout.
				stripped := stripAnsiCodes(line)
				for _, pat := range knownErrorPatterns {
					if pat.MatchString(stripped) {
						addError(stripped)
						cancel()
						return
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Monitor stderr — captures logs, classifies errors as fail-fast, and caches lines in buffer.
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

				// Classify errors from stderr under --print-logs
				if errMsg, ok := classifyError(line); ok {
					mu.Lock()
					detectedError = errMsg
					if strings.Contains(errMsg, "Quota/Rate Limit Exceeded") {
						rateLimitDetected = true
					}
					mu.Unlock()
					cancel()
					return
				}

				// Plain text matching on stderr as a fallback
				stripped := stripAnsiCodes(line)
				for _, pat := range knownErrorPatterns {
					if pat.MatchString(stripped) {
						addError(stripped)
						cancel()
						return
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Watchdog goroutine: Poll every 2 seconds to check for inactivity or global timeout
	watchdogDone := make(chan struct{})
	var inactivityTimeout bool
	var globalTimeout bool
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
					mu.Lock()
					inactivityTimeout = true
					mu.Unlock()
					cancel()
					return
				}
				if time.Since(start) > timeout {
					mu.Lock()
					globalTimeout = true
					mu.Unlock()
					cancel()
					return
				}
			}
		}
	}()

	runErr := cmd.Wait()
	close(watchdogDone)
	wg.Wait() // ensure both goroutines have finished draining pipes
	elapsed := time.Since(start)

	mu.Lock()
	isRateLimit := rateLimitDetected
	isInactivity := inactivityTimeout
	isGlobalTimeout := globalTimeout
	apiErr := detectedError
	errors := unique(detectedLines)
	mu.Unlock()

	// ── Rate Limit Error ───────────────────────────────────────────────────────
	if isRateLimit {
		return elapsed, fmt.Errorf("API Quota/Rate Limit Exceeded. Process aborted to prevent hang.")
	}

	// ── Classified API Error ───────────────────────────────────────────────────
	if apiErr != "" {
		return elapsed, fmt.Errorf("%s", apiErr)
	}

	// ── Inactivity Timeout ─────────────────────────────────────────────────────
	if isInactivity {
		return elapsed, fmt.Errorf(
			"Inactivity timeout: No response/output received from the model for 2 minutes.\n" +
				"  • The API might be overloaded or the task is too complex.\n" +
				"  • Switch to a faster model or try again later.",
		)
	}

	// ── Global Timeout ────────────────────────────────────────────────────────
	if isGlobalTimeout {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Timed out after %s (limit: %s)\n", elapsed.Round(time.Second), timeout))
		if len(errors) > 0 {
			sb.WriteString("\nErrors detected during execution:\n  • ")
			sb.WriteString(strings.Join(errors, "\n  • "))
		} else {
			sb.WriteString("\nThe model may be overloaded or the task too large. Consider:")
			sb.WriteString("\n  • Increasing 'timeout' in agents.yml")
			sb.WriteString("\n  • Switching to a faster model for this agent")
		}
		return elapsed, fmt.Errorf("%s", sb.String())
	}

	// ── Exit with error ────────────────────────────────────────────────────────
	if runErr != nil {
		stderrMu.Lock()
		stderrLines := strings.TrimSpace(stderrBuf.String())
		stderrMu.Unlock()

		if len(errors) > 0 {
			return elapsed, fmt.Errorf(
				"failed after %s:\n  • %s",
				elapsed.Round(time.Second), strings.Join(errors, "\n  • "),
			)
		}
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
				return elapsed, fmt.Errorf("failed after %s: %s", elapsed.Round(time.Second), lastLine)
			}
		}
		return elapsed, fmt.Errorf("failed after %s: %w", elapsed.Round(time.Second), runErr)
	}

	// ── Exit 0 but with suspicious output ─────────────────────────────────────
	if len(errors) > 0 {
		return elapsed, fmt.Errorf(
			"completed with warnings after %s:\n  • %s\n\nOutput may be incomplete — review and re-run if needed.",
			elapsed.Round(time.Second), strings.Join(errors, "\n  • "),
		)
	}

	return elapsed, nil
}

// unique deduplicates a slice of strings preserving order.
func unique(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// stripAnsiCodes removes ANSI escape sequences for clean regex matching.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripAnsiCodes(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
