package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"
	"harness-cli/internal/config"
	"harness-cli/internal/executor"
	"harness-cli/internal/setup"
	"harness-cli/internal/state"
	"harness-cli/internal/ui"
)

const tasksPath = ".harness/state/tasks.json"

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: smidhus-harness <command> [arguments]")
		fmt.Println("Commands:")
		fmt.Println("  init [path]   - Initializes the Harness environment in a project (default: current dir)")
		fmt.Println("  run           - Runs the state machine loop")
		os.Exit(1)
	}

	if err := setup.CheckOpenCode(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		targetPath := "."
		if len(os.Args) >= 3 {
			targetPath = os.Args[2]
		}
		if err := setup.InitProject(targetPath); err != nil {
			ui.PrintError(fmt.Sprintf("Error initializing: %v", err))
			os.Exit(1)
		}

	case "run":
		runLoop()

	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}
}

// runLoop is the deterministic state machine orchestrator.
// It owns all state transitions and persists them to tasks.json after each step.
func runLoop() {
	fmt.Print("\033[H\033[2J")
	ui.PrintBanner()

	for {
		// ── 1. Re-read state and config from disk at the top of every iteration ──
		projectState, err := state.LoadState(tasksPath)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Error reading tasks.json: %v", err))
			os.Exit(1)
		}

		cfg, err := config.LoadConfig(".harness/agents.yml")
		if err != nil {
			ui.PrintError(fmt.Sprintf("Error reading agents.yml: %v", err))
			os.Exit(1)
		}

		// ── 2. Find the first non-done task ───────────────────────────────────
		var activeTask *state.Task
		for i := range projectState.Tasks {
			if projectState.Tasks[i].Status != "done" {
				activeTask = &projectState.Tasks[i]
				break
			}
		}

		if activeTask == nil {
			ui.PrintSuccess("All tasks completed. The harness has finished its work. 🎉")
			break
		}

		// Pretty-print active task title
		title := activeTask.Title
		if len(title) > 55 {
			title = title[:52] + "..."
		}
		ui.PrintKeyValue("Task", title)
		ui.PrintKeyValue("Status", activeTask.Status)

		// ── 3. Determine which agent to run based on current status ───────────
		var targetAgent string

		switch activeTask.Status {

		case "pending":
			// Architect analyses requirements and produces specs.
			targetAgent = "architect"

		case "spec_ready":
			// Human-in-the-loop: print the pause box once, then poll until approved.
			pauseBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(ui.Primary)).
				Padding(1, 3).
				Render(
					ui.PrimaryText.Render("⏸  Paused — Architect finished the specs") + "\n\n" +
						ui.BaseText.Render("1. Review the spec files in ") + ui.PrimaryText.Render(".harness/specs/") + "\n" +
						ui.BaseText.Render("2. Edit them if needed\n") +
						ui.BaseText.Render("3. Change task status to ") + ui.PrimaryText.Render(`"approved"`) +
						ui.BaseText.Render(" in ") + ui.PrimaryText.Render("tasks.json") +
						ui.BaseText.Render(" to continue"),
				)
			fmt.Println("\n" + pauseBox)

			// Poll every 3 seconds until the user edits tasks.json.
			waitStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Muted)).Italic(true)
			dots := []string{"·  ", "·· ", "···"}
			tick := 0
			for {
				time.Sleep(3 * time.Second)
				tick++
				fmt.Printf("\r%s", waitStyle.Render(fmt.Sprintf("Waiting for approval %s", dots[tick%3])))

				fresh, err := state.LoadState(tasksPath)
				if err != nil {
					continue // transient read error, keep waiting
				}
				for i := range fresh.Tasks {
					if fresh.Tasks[i].ID == activeTask.ID && fresh.Tasks[i].Status != "spec_ready" {
						fmt.Println() // newline after the inline waiting indicator
						ui.PrintKeyValue("Detected", "Status changed to '"+fresh.Tasks[i].Status+"' — resuming...")
						goto continueLoop
					}
				}
			}
		continueLoop:
			continue

		case "approved", "in_progress":
			// Ensure there's at least a default agent list.
			if len(activeTask.RequiredAgents) == 0 {
				activeTask.RequiredAgents = []string{"builder"}
			}

			// Check if all builders are done → transition to reviewing.
			if activeTask.AgentIndex >= len(activeTask.RequiredAgents) {
				ui.PrintKeyValue("Progress", "All builders finished → moving to reviewing")
				activeTask.Status = "reviewing"
				activeTask.AgentIndex = 0
				if err := state.SaveState(tasksPath, projectState); err != nil {
					ui.PrintError(fmt.Sprintf("Error saving state: %v", err))
					os.Exit(1)
				}
				continue // restart loop, will now hit "reviewing" case
			}

			targetAgent = activeTask.RequiredAgents[activeTask.AgentIndex]

		case "reviewing":
			targetAgent = "gatekeeper"

		case "documenting":
			targetAgent = "documenter"

		default:
			ui.PrintError(fmt.Sprintf("Unknown task status: '%s'. Edit tasks.json to fix.", activeTask.Status))
			os.Exit(1)
		}

		// ── 4. Resolve agent prompt (local file takes precedence over embedded) ──
		agentContent, err := resolveAgentContent(targetAgent)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}

		agentCfg, exists := cfg.Agents[targetAgent]
		if !exists {
			ui.PrintError(fmt.Sprintf("Agent '%s' has no model assigned in agents.yml", targetAgent))
			os.Exit(1)
		}

		ui.PrintKeyValue("Agent", fmt.Sprintf("%s  ·  %s", targetAgent, agentCfg.Model))

		// ── 5. Run agent synchronously ──────────────────────────────────────────
		var elapsed time.Duration
		var runErr error

		elapsed, runErr = executor.RunAgent(
			targetAgent,
			agentCfg,
			string(agentContent),
			cfg.GlobalSettings.Timeout(),
		)

		if runErr != nil {
			// Print a styled error panel with the specific message from opencode.
			errBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(ui.Error)).
				Padding(1, 3).
				Render(
					ui.ErrorText.Render("✗  Agent failed: "+targetAgent) + "\n\n" +
						ui.BaseText.Render(runErr.Error()) + "\n\n" +
						ui.MutedText.Render("State was NOT advanced. Press Enter to retry, or Ctrl+C to abort."),
				)
			fmt.Println("\n" + errBox)

			// Block waiting for the user to press Enter to retry.
			fmt.Scanln()
			continue // retry the same agent without changing state
		}

		ui.PrintKeyValue("Result", ui.SuccessText.Render(fmt.Sprintf("✓ Done (%s)", elapsed.Round(time.Millisecond))))

		// ── 6. Go transitions the state — deterministically, after success ─────
		switch activeTask.Status {

		case "pending":
			// Architect succeeded → wait for human review of specs.
			activeTask.Status = "spec_ready"

		case "approved", "in_progress":
			// Mark as in_progress so it's clear the cycle has started.
			activeTask.Status = "in_progress"
			// Advance the builder index.
			activeTask.AgentIndex++
			// If all builders done, transition will happen at top of next loop.

		case "reviewing":
			// Gatekeeper passed → generate docs.
			activeTask.Status = "documenting"

		case "documenting":
			// Documenter finished → mark as done.
			activeTask.Status = "done"
		}

		// ── 7. Persist the updated state immediately ───────────────────────────
		if err := state.SaveState(tasksPath, projectState); err != nil {
			ui.PrintError(fmt.Sprintf("Error saving state: %v", err))
			os.Exit(1)
		}

		fmt.Println()
		time.Sleep(1 * time.Second)
	}
}

// resolveAgentContent loads the agent prompt from .harness/agents/<name>.md if it
// exists, otherwise falls back to the embedded template shipped with the binary.
func resolveAgentContent(agentName string) ([]byte, error) {
	localPath := filepath.Join(".harness", "agents", agentName+".md")
	if _, err := os.Stat(localPath); err == nil {
		data, err := os.ReadFile(localPath)
		if err != nil {
			return nil, fmt.Errorf("error reading local agent file %s: %w", localPath, err)
		}
		return data, nil
	}

	data, err := setup.TemplatesFS.ReadFile(fmt.Sprintf("templates/%s.md", agentName))
	if err != nil {
		return nil, fmt.Errorf("no agent prompt found for '%s' — add .harness/agents/%s.md or an embedded template", agentName, agentName)
	}
	return data, nil
}