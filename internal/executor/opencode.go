package executor

import (
	"fmt"
	"os"
	"os/exec"

	"harness-cli/internal/config"
)

// RunAgent invokes OpenCode with the correct model and agent prompt
func RunAgent(agentName string, cfg config.AgentConfig, agentContent string) error {
	fmt.Printf("🚀 Executing agent: [%s] with model [%s]\n", agentName, cfg.Model)

	// 1. Build the command correctly for OpenCode
	// We use "run" and pass the MD content as the message/prompt.
	// We add --dangerously-skip-permissions for autonomous execution without permission interruptions.
	cmd := exec.Command("opencode", "run", "--model", cfg.Model, "--dangerously-skip-permissions", agentContent)

	// 2. Connect pipes to see real-time output
	// We do not assign cmd.Stdin so it runs non-interactively (as if coming from /dev/null)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// 3. Execute
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("critical failure in agent %s: %w", agentName, err)
	}

	return nil
}