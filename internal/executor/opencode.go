package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"harness-cli/internal/config"
)

// RunAgent invokes OpenCode with the correct model and agent prompt.
// All three stdio streams are passed through so the user sees the model's
// thinking in real time. The call is bounded by the provided timeout.
func RunAgent(agentName string, cfg config.AgentConfig, agentContent string, timeout time.Duration) error {
	fmt.Printf("\n┌─────────────────────────────────────────────────────────────────┐\n")
	fmt.Printf("│  🤖 Agent: %-20s  Model: %-22s│\n", agentName, cfg.Model)
	fmt.Printf("│  ⏱  Timeout: %-10s                                          │\n", timeout)
	fmt.Printf("└─────────────────────────────────────────────────────────────────┘\n\n")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "opencode", "run",
		"--model", cfg.Model,
		"--dangerously-skip-permissions",
		"--thinking",
		agentContent,
	)

	// Pass through all three streams so the user sees the model's full output
	// (thinking, tool calls, responses) exactly as OpenCode renders them.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	fmt.Printf("\n┌─────────────────────────────────────────────────────────────────┐\n")
	if ctx.Err() == context.DeadlineExceeded {
		fmt.Printf("│  ⏰ Agent [%s] timed out after %s\n", agentName, elapsed.Round(time.Millisecond))
		fmt.Printf("└─────────────────────────────────────────────────────────────────┘\n\n")
		return fmt.Errorf("agent %s timed out after %s (limit: %s)", agentName, elapsed.Round(time.Millisecond), timeout)
	}
	if err != nil {
		fmt.Printf("│  ❌ Agent [%s] failed after %s\n", agentName, elapsed.Round(time.Millisecond))
		fmt.Printf("└─────────────────────────────────────────────────────────────────┘\n\n")
		return fmt.Errorf("critical failure in agent %s after %s: %w", agentName, elapsed.Round(time.Millisecond), err)
	}

	fmt.Printf("│  ✅ Agent [%s] finished in %s\n", agentName, elapsed.Round(time.Millisecond))
	fmt.Printf("└─────────────────────────────────────────────────────────────────┘\n\n")
	return nil
}