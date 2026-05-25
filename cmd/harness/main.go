package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"harness-cli/internal/config"
	"harness-cli/internal/executor"
	"harness-cli/internal/setup"
	"harness-cli/internal/state"
	"harness-cli/internal/ui"
)

const tasksPath = ".harness/state/tasks.json"

var (
	version = "dev"
	commit  = "none"
)

func printHelp() {
	ui.PrintBanner()

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary)).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Text))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Muted))
	boldStyle := lipgloss.NewStyle().Bold(true)

	printRow := func(label, desc string, labelStyle lipgloss.Style) {
		const targetWidth = 16
		padding := ""
		if len(label) < targetWidth {
			padding = strings.Repeat(" ", targetWidth-len(label))
		}
		fmt.Printf("  %s%s%s\n", labelStyle.Render(label), padding, descStyle.Render(desc))
	}

	fmt.Println(titleStyle.Render("Smidhus Harness") + " - " + descStyle.Render("Deterministic CLI orchestrator for AI agents with Opencode"))
	fmt.Println()
	fmt.Println(boldStyle.Render("Usage:"))
	fmt.Println("  smidhus-harness <command> [arguments]")
	fmt.Println()
	fmt.Println(boldStyle.Render("Commands:"))
	printRow("init [path]", "Initializes the Harness environment in a project (default: current dir)", titleStyle)
	printRow("run", "Runs the state machine loop", titleStyle)
	printRow("version", "Displays the current version and commit hash", titleStyle)
	printRow("help", "Displays this help menu", titleStyle)
	fmt.Println()
	fmt.Println(boldStyle.Render("Flags / Global Options:"))
	printRow("-v, --version", "Display version information", mutedStyle)
	printRow("-h, --help", "Display help and usage guidelines", mutedStyle)
	fmt.Println()
}

func printVersion() {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Primary)).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Text))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.Muted))

	fmt.Printf("%s version %s (commit: %s)\n", titleStyle.Render("smidhus-harness"), descStyle.Render(version), mutedStyle.Render(commit))
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	arg1 := strings.ToLower(os.Args[1])
	if arg1 == "-v" || arg1 == "--version" || arg1 == "version" {
		printVersion()
		os.Exit(0)
	}
	if arg1 == "-h" || arg1 == "--help" || arg1 == "help" {
		printHelp()
		os.Exit(0)
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
		runCmdCreator := func(p *tea.Program, retryChan chan struct{}) tea.Cmd {
			return func() tea.Msg {
				go runLoop(p, retryChan)
				return ui.RunStartedMsg{}
			}
		}
		if err := setup.InitProject(targetPath, runCmdCreator); err != nil {
			ui.PrintError(fmt.Sprintf("Error initializing: %v", err))
			os.Exit(1)
		}

	case "run":
		retryChan := make(chan struct{})
		model := ui.NewTUIModel(retryChan)
		p := tea.NewProgram(model, tea.WithAltScreen())

		ui.TUILogCallback = func(msg string) {
			p.Send(ui.LogMsg(msg + "\n"))
		}

		model.RunCmd = func() tea.Msg {
			go runLoop(p, retryChan)
			return ui.RunStartedMsg{}
		}

		go runLoop(p, retryChan)

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}
}

// runLoop is the deterministic state machine orchestrator.
// State transitions are owned exclusively by Go — never delegated to the AI.
// Both tasks.json and agents.yml are reloaded each iteration so the user can
// edit them while the harness is paused without restarting.
func runLoop(p *tea.Program, retryChan chan struct{}) {
	for {
		// Reload from disk every tick so mid-run edits are picked up immediately.
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

		// Scan sequentially: the harness works one task at a time.
		var activeTask *state.Task
		for i := range projectState.Tasks {
			if projectState.Tasks[i].Status != "done" {
				activeTask = &projectState.Tasks[i]
				break
			} else {
				// Safety check: archive any leftover spec files if a task is marked done
				archiveTaskSpecs(projectState.Tasks[i].ID, p)
			}
		}

		if activeTask == nil {
			ui.PrintSuccess("All tasks completed. The forge is done.")
			if p != nil {
				p.Send(ui.FinishedMsg{})
			}
			break
		}

		// Pretty-print active task description
		desc := activeTask.Description
		if len(desc) > 55 {
			desc = desc[:52] + "..."
		}
		ui.PrintKeyValue("Task", desc)
		ui.PrintKeyValue("Status", activeTask.Status)

		// Agent selection is status-driven to keep the orchestration deterministic.
		var targetAgent string

		switch activeTask.Status {

		case "pending":
			// Architect is always first: it produces the spec files the other agents depend on.
			targetAgent = "architect"

		case "spec_ready":
			// Human-in-the-loop gate. Poll tasks.json until the user advances the status.
			pauseBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(ui.Primary)).
				Padding(1, 3).
				Render(
					strings.Join([]string{
						ui.PrimaryText.Render("[!] Paused -- Architect finished the specs"),
						"",
						ui.BaseText.Render("1. Review the spec files in ") + ui.PrimaryText.Render(".harness/specs/"),
						ui.BaseText.Render("2. Edit them if needed"),
						ui.BaseText.Render("3. Change task status to ") + ui.PrimaryText.Render(`"approved"`) + ui.BaseText.Render(" in ") + ui.PrimaryText.Render("tasks.json") + ui.BaseText.Render(" to continue"),
					}, "\n"),
				)

			if p != nil {
				p.Send(ui.SetAgentMsg{Agent: "", Skill: ""})
				p.Send(ui.LogMsg("\n" + pauseBox + "\n"))

				for {
					time.Sleep(3 * time.Second)
					fresh, err := state.LoadState(tasksPath)
					if err != nil {
						continue
					}
					for i := range fresh.Tasks {
						if fresh.Tasks[i].ID == activeTask.ID && fresh.Tasks[i].Status != "spec_ready" {
							ui.PrintKeyValue("Detected", "Status changed to '"+fresh.Tasks[i].Status+"' — resuming...")
							goto continueLoop
						}
					}
				}
			} else {
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
			}
		continueLoop:
			continue

		case "approved", "in_progress":
			// Default to builder when the spec doesn't name specific agents.
			if len(activeTask.RequiredAgents) == 0 {
				activeTask.RequiredAgents = []string{"builder"}
			}

			// All required agents have run — advance to the review gate.
			if activeTask.AgentIndex >= len(activeTask.RequiredAgents) {
				ui.PrintKeyValue("Progress", "All builders finished -> moving to reviewing")
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

		// Local agent files override the embedded defaults, allowing per-project customization.
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
			p,
		)

		if runErr != nil {
			// State is intentionally NOT advanced on failure so the user can fix the
			// underlying issue (e.g. swap models, edit the prompt) and press Enter to retry.
			wrapW := ui.GetTerminalWidth() - 10
			if wrapW < 20 {
				wrapW = 20
			}
			wrappedErr := ui.WrapText(runErr.Error(), wrapW)
			errBox := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(ui.Error)).
				Padding(1, 3).
				Render(
					ui.ErrorText.Render("[x] Agent failed: "+targetAgent) + "\n\n" +
						ui.BaseText.Render(wrappedErr) + "\n\n" +
						ui.MutedText.Render("State was NOT advanced. Press Enter to retry, or q to abort."),
				)
			
			if p != nil {
				p.Send(ui.SetAgentMsg{Agent: "", Skill: ""})
				p.Send(ui.LogMsg("\n" + errBox + "\n"))
				
				// Drain any old retry signal
				select {
				case <-retryChan:
				default:
				}
				// Wait for TUI retry command (Enter key)
				<-retryChan
			} else {
				fmt.Println("\n" + errBox)
				_, _ = fmt.Scanln()
			}
			continue
		}

		ui.PrintKeyValue("Result", ui.SuccessText.Render(fmt.Sprintf("[+] Done (%s)", elapsed.Round(time.Millisecond))))

		// State transitions are intentionally centralized here, not inside agents.
		switch activeTask.Status {

		case "pending":
			// Specs are now ready for human review before any code is written.
			activeTask.Status = "spec_ready"

		case "approved", "in_progress":
			activeTask.Status = "in_progress"
			activeTask.AgentIndex++

		case "reviewing":
			activeTask.Status = "documenting"

		case "documenting":
			activeTask.Status = "done"
		}

		// Persist immediately so a crash never rolls back a completed step.
		if err := state.SaveState(tasksPath, projectState); err != nil {
			ui.PrintError(fmt.Sprintf("Error saving state: %v", err))
			os.Exit(1)
		}

		if p == nil {
			fmt.Println()
		}
		time.Sleep(1 * time.Second)
	}
}

// resolveAgentContent gives users a local override mechanism: drop a custom
// <name>.md under .harness/agents/ and it takes precedence over the embedded default.
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

// archiveTaskSpecs cleans up and archives specs for completed tasks (safety net for manual bypasses).
func archiveTaskSpecs(taskID string, p *tea.Program) {
	specsDir := filepath.Join(".harness", "specs")
	archiveDir := filepath.Join(specsDir, "archive")

	entries, err := os.ReadDir(specsDir)
	if err != nil {
		return // specs directory might not exist yet
	}

	var filesToMove []string
	prefix := taskID + "_"
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			filesToMove = append(filesToMove, entry.Name())
		}
	}

	if len(filesToMove) == 0 {
		return
	}

	// Create archive directory if needed
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return
	}

	logMsg := fmt.Sprintf(" !  [%s] Unarchived specs detected. Moving to specs/archive...", taskID)
	if p != nil {
		p.Send(ui.LogMsg(logMsg + "\n"))
	} else {
		fmt.Println(logMsg)
	}

	for _, filename := range filesToMove {
		src := filepath.Join(specsDir, filename)
		dst := filepath.Join(archiveDir, filename)
		if err := os.Rename(src, dst); err != nil {
			// Fallback: Copy and delete
			data, err := os.ReadFile(src)
			if err == nil {
				if os.WriteFile(dst, data, 0644) == nil {
					_ = os.Remove(src)
				}
			}
		}
	}
}