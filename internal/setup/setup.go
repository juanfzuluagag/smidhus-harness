package setup

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/*.md
var TemplatesFS embed.FS

// detectProject scans the target project path to deduce name, stack and test commands
func detectProject(targetPath string) (name string, stack string, depManager string, testCmd string, lintCmd string) {
	// Get default base name
	absPath, err := filepath.Abs(targetPath)
	if err == nil {
		name = filepath.Base(absPath)
	} else {
		name = "Project"
	}

	stack = "Generic / Unknown"
	depManager = "None"
	testCmd = "# Define your test command here"
	lintCmd = "# Define your linter command here"

	// 1. Detect Go
	if _, err := os.Stat(filepath.Join(targetPath, "go.mod")); err == nil {
		stack = "Go"
		depManager = "go modules"
		testCmd = "go test ./..."
		lintCmd = "go vet ./..."

		// Attempt to read the module name in go.mod
		if content, err := os.ReadFile(filepath.Join(targetPath, "go.mod")); err == nil {
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					name = strings.TrimPrefix(line, "module ")
					name = strings.TrimSpace(name)
					break
				}
			}
		}
		return
	}

	// 2. Detect Node.js (JS/TS)
	if _, err := os.Stat(filepath.Join(targetPath, "package.json")); err == nil {
		stack = "Node.js (JavaScript/TypeScript)"
		depManager = "npm"
		testCmd = "npm test"
		lintCmd = "npm run lint"

		// Attempt to read the name in package.json
		if content, err := os.ReadFile(filepath.Join(targetPath, "package.json")); err == nil {
			var pkg struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(content, &pkg) == nil && pkg.Name != "" {
				name = pkg.Name
			}
		}

		// Adjust according to package manager
		if _, err := os.Stat(filepath.Join(targetPath, "yarn.lock")); err == nil {
			depManager = "yarn"
			testCmd = "yarn test"
			lintCmd = "yarn lint"
		} else if _, err := os.Stat(filepath.Join(targetPath, "pnpm-lock.yaml")); err == nil {
			depManager = "pnpm"
			testCmd = "pnpm test"
			lintCmd = "pnpm lint"
		}
		return
	}

	// 3. Detect Rust
	if _, err := os.Stat(filepath.Join(targetPath, "Cargo.toml")); err == nil {
		stack = "Rust"
		depManager = "cargo"
		testCmd = "cargo test"
		lintCmd = "cargo clippy"

		// Attempt to read the name in Cargo.toml
		if content, err := os.ReadFile(filepath.Join(targetPath, "Cargo.toml")); err == nil {
			lines := strings.Split(string(content), "\n")
			inPackageSection := false
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "[package]" {
					inPackageSection = true
					continue
				}
				if strings.HasPrefix(line, "[") {
					inPackageSection = false
				}
				if inPackageSection && strings.HasPrefix(line, "name") {
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						n := strings.TrimSpace(parts[1])
						n = strings.Trim(n, "\"")
						n = strings.Trim(n, "'")
						if n != "" {
							name = n
						}
					}
					break
				}
			}
		}
		return
	}

	// 4. Detect Python
	if _, err := os.Stat(filepath.Join(targetPath, "requirements.txt")); err == nil {
		stack = "Python"
		depManager = "pip"
		testCmd = "pytest"
		lintCmd = "flake8"
		return
	} else if _, err := os.Stat(filepath.Join(targetPath, "pyproject.toml")); err == nil {
		stack = "Python"
		depManager = "poetry"
		testCmd = "poetry run pytest"
		lintCmd = "poetry run black --check ."
		return
	}

	return
}

// InitProject creates the harness structure in the indicated path
func InitProject(targetPath string) error {
	harnessDir := filepath.Join(targetPath, ".harness")

	// Detect project characteristics
	projName, techStack, depManager, testCmd, lintCmd := detectProject(targetPath)

	// 1. Create folder structure
	dirs := []string{
		filepath.Join(harnessDir, "state"),
		filepath.Join(harnessDir, "specs"),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("error creating folder %s: %v", d, err)
		}
	}

	// 3. Create the tasks JSON with the detected project name and an initial task
	tasksJSON := fmt.Sprintf(`{
  "project": "%s",
  "tasks": [
    {
      "id": "T001",
      "title": "Initialize the development harness and validate the environment",
      "status": "pending"
    }
  ]
}`, projName)
	os.WriteFile(filepath.Join(harnessDir, "state", "tasks.json"), []byte(tasksJSON), 0644)

	// 4. Create the model configuration YAML with the correct "google/" prefix
	agentsYAML := `global_settings:
  timeout: 300
agents:
  architect:
    model: "google/gemini-2.5-flash"
  builder:
    model: "google/gemini-2.5-flash"
  gatekeeper:
    model: "google/gemini-2.5-flash"
`
	os.WriteFile(filepath.Join(harnessDir, "agents.yml"), []byte(agentsYAML), 0644)

	// 5. Create the customized Blueprint (Manifest) dynamically
	blueprintMD := fmt.Sprintf(`# Project Manifest (Blueprint)

## 1. Base Technology Stack
- **Name**: %s
- **Language/Stack**: %s
- **Dependency Manager**: %s

## 2. Code and Style Conventions
- Follow official guidelines and recommended conventions for %s.
- All new code must go in the appropriate root directories.

## 3. Testing Strategy
- Continuous automated tests running the local test suite.

## 4. Local Validation Pipeline (Commands)
- Test command: %s
- Linter/analysis command: %s
`, projName, techStack, depManager, techStack, testCmd, lintCmd)
	os.WriteFile(filepath.Join(harnessDir, "blueprint.md"), []byte(blueprintMD), 0644)

	// Return successfully

	fmt.Printf("✅ Harness environment successfully initialized in: %s\n", harnessDir)
	fmt.Printf("👉 Detected Stack: %s (Manager: %s)\n", techStack, depManager)
	fmt.Println("👉 Next step: Edit .harness/blueprint.md and define your additional rules.")
	return nil
}