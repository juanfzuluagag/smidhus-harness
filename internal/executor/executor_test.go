package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"harness-cli/internal/config"
)

func TestFormatErrorLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Normal line",
			input:    "No error here",
			expected: "No error here",
		},
		{
			name:     "Truncated long line",
			input:    strings.Repeat("B", 600),
			expected: strings.Repeat("B", 497) + "...",
		},
		{
			name:     "Structured flat error json parsing",
			input:    `[INFO] error={"name":"UnknownError","statusCode":500,"type":"UnexpectedError","message":"Server is overloaded"}`,
			expected: `[INFO] error={Name: UnknownError, StatusCode: 500, Type: UnexpectedError, Message: Server is overloaded}`,
		},
		{
			name:     "Structured nested error json parsing",
			input:    `[INFO] error={"error":{"name":"APIError","statusCode":429,"type":"RateLimit","message":"Too many requests"}}`,
			expected: `[INFO] error={Name: APIError, StatusCode: 429, Type: RateLimit, Message: Too many requests}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatErrorLine(tt.input)
			if got != tt.expected {
				t.Errorf("formatErrorLine() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedMsg   string
		expectedFound bool
	}{
		{
			name:          "Non-error log line",
			input:         "INFO: Running main loops",
			expectedMsg:   "",
			expectedFound: false,
		},
		{
			name:          "Rate limit token usage_limit_reached",
			input:         "ERROR level:50 usage_limit_reached",
			expectedMsg:   "API Quota/Rate Limit Exceeded. Process aborted to prevent hang.",
			expectedFound: true,
		},
		{
			name:          "Unexpected server error token",
			input:         "ERROR level:50 unexpected server error",
			expectedMsg:   "Unexpected server error. Process aborted.",
			expectedFound: true,
		},
		{
			name:          "Context length token",
			input:         "ERROR: context length exceeded",
			expectedMsg:   "Context window limit exceeded. Process aborted.",
			expectedFound: true,
		},
		{
			name:          "General API call error fallback",
			input:         "ERROR: api_apicallerror happened",
			expectedMsg:   "API Call Error. Process aborted to prevent hang.",
			expectedFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMsg, gotFound := classifyError(tt.input)
			if gotFound != tt.expectedFound {
				t.Fatalf("classifyError() found = %v, want %v", gotFound, tt.expectedFound)
			}
			if gotMsg != tt.expectedMsg {
				t.Errorf("classifyError() msg = %q, want %q", gotMsg, tt.expectedMsg)
			}
		})
	}
}

func TestUnique(t *testing.T) {
	input := []string{"error1", "error2", "error1", "error3", "error2"}
	expected := []string{"error1", "error2", "error3"}
	got := unique(input)

	if len(got) != len(expected) {
		t.Fatalf("unique() returned slice of len %d, want %d", len(got), len(expected))
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Errorf("unique()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestStripAnsiCodes(t *testing.T) {
	input := "\x1b[31mRed Error\x1b[0m and \x1b[1mBold Text\x1b[0m"
	expected := "Red Error and Bold Text"
	got := stripAnsiCodes(input)
	if got != expected {
		t.Errorf("stripAnsiCodes() = %q, want %q", got, expected)
	}
}

func TestRunAgentWithMockBinary(t *testing.T) {
	// Create a temp directory for compiling the mock opencode binary
	tmpDir, err := os.MkdirTemp("", "opencode-mock-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write mock opencode source code
	mockSrc := `package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(1)
	}
	prompt := os.Args[len(os.Args)-1]

	if strings.Contains(prompt, "SUCCESS") {
		fmt.Println(` + "`" + `{"type":"assistant"}` + "`" + `)
		fmt.Println(` + "`" + `{"type":"step_start"}` + "`" + `)
		fmt.Println(` + "`" + `{"type":"reasoning","part":{"text":"thinking about success..."}}` + "`" + `)
		fmt.Println(` + "`" + `{"type":"step_finish"}` + "`" + `)
		os.Exit(0)
	}

	if strings.Contains(prompt, "RATELIMIT") {
		fmt.Fprintln(os.Stderr, "ERROR level:50 error=rate_limit_exceeded Message=Quota exceeded")
		os.Exit(1)
	}

	if strings.Contains(prompt, "SLEEP") {
		time.Sleep(4 * time.Second)
		os.Exit(0)
	}

	fmt.Fprintln(os.Stderr, "unknown mock instruction")
	os.Exit(1)
}
`
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(mockSrc), 0644); err != nil {
		t.Fatalf("failed to write mock main.go: %v", err)
	}

	// Compile mock binary
	binName := "opencode"
	if filepath.Separator == '\\' {
		binName += ".exe"
	}
	binPath := filepath.Join(tmpDir, binName)

	buildCtx, buildCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer buildCancel()
	cmdBuild := exec.CommandContext(buildCtx, "go", "build", "-o", binPath, srcPath)
	if err := cmdBuild.Run(); err != nil {
		t.Fatalf("failed to compile mock opencode: %v", err)
	}

	// Prepend temp directory to PATH so Go exec finds our mock opencode binary first
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+string(os.PathListSeparator)+origPath)
	defer os.Setenv("PATH", origPath)

	cfg := config.AgentConfig{
		Model: "mock-model",
	}

	t.Run("Success agent run", func(t *testing.T) {
		elapsed, err := RunAgent("builder", cfg, "SUCCESS", 5*time.Second)
		if err != nil {
			t.Fatalf("unexpected execution failure: %v", err)
		}
		if elapsed <= 0 {
			t.Error("expected elapsed time to be positive")
		}
	})

	t.Run("Fail fast rate limit", func(t *testing.T) {
		_, err := RunAgent("builder", cfg, "RATELIMIT", 5*time.Second)
		if err == nil {
			t.Fatal("expected execution failure, got nil")
		}
		if !strings.Contains(err.Error(), "API Quota/Rate Limit Exceeded") {
			t.Errorf("expected error message to contain rate limit, got: %v", err)
		}
	})

	t.Run("Global timeout triggers cancel", func(t *testing.T) {
		_, err := RunAgent("builder", cfg, "SLEEP", 50*time.Millisecond)
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
		if !strings.Contains(err.Error(), "Timed out after") {
			t.Errorf("expected error message to contain timeout info, got: %v", err)
		}
	})
}
