package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"harness-cli/internal/config"
	"harness-cli/internal/executor"
	"harness-cli/internal/setup"
	"harness-cli/internal/state"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: smidhus-harness <command> [arguments]")
		fmt.Println("Commands:")
		fmt.Println("  init <path>   - Initializes the Harness environment in a project")
		fmt.Println("  run           - Runs the state machine loop")
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		if len(os.Args) < 3 {
			log.Fatal("Missing project path. Usage: smidhus-harness init /path/to/project")
		}
		targetPath := os.Args[2]
		if err := setup.InitProject(targetPath); err != nil {
			log.Fatalf("Error initializing: %v", err)
		}

	case "run":
		fmt.Println("🚀 Starting Harness Engine...")

		// 1. Load the model configuration (The YAML)
		cfg, err := config.LoadConfig(".harness/agents.yml")
		if err != nil {
			log.Fatalf("❌ Error reading agents.yml: %v", err)
		}

		for {
			// 2. Read the current project state (The JSON)
			projectState, err := state.LoadState(".harness/state/tasks.json")
			if err != nil {
				log.Fatalf("❌ Error reading tasks.json: %v", err)
			}

			// 3. Find the active task
			var activeTask *state.Task
			for i := range projectState.Tasks {
				if projectState.Tasks[i].Status != "done" {
					activeTask = &projectState.Tasks[i]
					break
				}
			}

			if activeTask == nil {
				fmt.Println("🎉 All tasks are 'done'. The harness has finished its work.")
				break
			}

			// 4. Determine which agent should act (State Machine)
			var targetAgent string

			switch activeTask.Status {
			case "pending":
				targetAgent = "architect"
			case "spec_ready":
				fmt.Println("⏸️  Paused: The Architect finished the specs in .harness/specs/.")
				fmt.Println("👉 Review the requirements. If you approve them, change the task status to 'approved' in tasks.json and run 'run' again.")
				return // Stops the program for Human-in-the-Loop
			case "approved", "in_progress":
				// We will launch the builder by default in this first test
				targetAgent = "builder"
			case "reviewing":
				targetAgent = "gatekeeper"
			case "documenting":
				targetAgent = "documenter"
			default:
				log.Fatalf("❌ Unknown status: %s", activeTask.Status)
			}

			// 5. Extract the configuration for that specific agent
			agentCfg, exists := cfg.Agents[targetAgent]
			if !exists {
				log.Fatalf("❌ The agent '%s' has no model assigned in agents.yml", targetAgent)
			}

			// 6. THE MAGIC! Go calls OpenCode injecting the model
			var agentContent []byte
			localAgentPath := fmt.Sprintf(".harness/agents/%s.md", targetAgent)

			if _, err := os.Stat(localAgentPath); err == nil {
				// Load local override if it exists
				agentContent, err = os.ReadFile(localAgentPath)
				if err != nil {
					log.Fatalf("❌ Error reading local agent file %s: %v", localAgentPath, err)
				}
			} else {
				// Fallback to embedded template
				agentContent, err = setup.TemplatesFS.ReadFile(fmt.Sprintf("templates/%s.md", targetAgent))
				if err != nil {
					log.Fatalf("❌ Error reading embedded agent template for %s: %v", targetAgent, err)
				}
			}

			fmt.Printf("🤖 Invoking [%s] using model [%s]...\n", targetAgent, agentCfg.Model)

			err = executor.RunAgent(targetAgent, agentCfg, string(agentContent))
			if err != nil {
				log.Fatalf("❌ OpenCode failed to execute %s: %v", targetAgent, err)
			}

			fmt.Println("✅ Agent execution finished. Re-evaluating state...")
			time.Sleep(2 * time.Second)
		}

	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}
}