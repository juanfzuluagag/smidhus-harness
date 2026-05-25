package skills

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"harness-cli/internal/ui"
)

// execCommand is a package-level variable allowing unit tests to override
// the command execution behavior with a mocked process.
var execCommand = exec.Command

// statFile is a package-level variable allowing unit tests to mock file existence checks.
var statFile = os.Stat

// isSkillInstalledFunc checks if a skill is installed either locally or globally.
var isSkillInstalledFunc = func(skillName string) bool {
	// 1. Check project-local paths
	localPaths := []string{
		filepath.Join(".opencode", "skills", skillName, "SKILL.md"),
		filepath.Join(".claude", "skills", skillName, "SKILL.md"),
		filepath.Join(".agents", "skills", skillName, "SKILL.md"),
	}
	for _, p := range localPaths {
		if _, err := statFile(p); err == nil {
			return true
		}
	}

	// 2. Check global paths under User Home directory
	home, err := os.UserHomeDir()
	if err == nil {
		globalPaths := []string{
			filepath.Join(home, ".config", "opencode", "skills", skillName, "SKILL.md"),
			filepath.Join(home, ".claude", "skills", skillName, "SKILL.md"),
			filepath.Join(home, ".agents", "skills", skillName, "SKILL.md"),
		}
		for _, p := range globalPaths {
			if _, err := statFile(p); err == nil {
				return true
			}
		}
	}
	return false
}

// parseSkillDeclaration splits a skill declaration string (e.g. "owner/repo/skill-name")
// into the repository path and the skill name.
func parseSkillDeclaration(decl string) (repo string, skillName string) {
	decl = strings.TrimSpace(decl)

	// Handle HTTP/HTTPS URLs
	if strings.HasPrefix(decl, "http://") || strings.HasPrefix(decl, "https://") {
		parts := strings.Split(decl, "/")
		if len(parts) >= 5 {
			repo = strings.Join(parts[:len(parts)-1], "/")
			skillName = parts[len(parts)-1]
			return
		}
	}

	// Handle standard owner/repo/skill-name
	parts := strings.Split(decl, "/")
	if len(parts) >= 3 {
		repo = strings.Join(parts[:len(parts)-1], "/")
		skillName = parts[len(parts)-1]
		return
	}

	// Handle standard owner/repo
	if len(parts) == 2 {
		repo = decl
		skillName = parts[1]
		return
	}

	// Simple name fallback
	return "", decl
}

// AutoEquip verifies if the declared skills are installed. If a skill is missing,
// it is automatically installed globally using the skills CLI via npx.
func AutoEquip(skills []string) error {
	for _, skillDecl := range skills {
		repo, skillName := parseSkillDeclaration(skillDecl)

		ui.PrintInfo(fmt.Sprintf("Checking if skill '%s' is installed...", skillName))
		if isSkillInstalledFunc(skillName) {
			ui.PrintSuccess(fmt.Sprintf("Skill '%s' is already installed.", skillName))
			continue
		}

		ui.PrintWarning(fmt.Sprintf("Skill '%s' not found.", skillName))

		if repo == "" {
			return fmt.Errorf("skill '%s' is not installed, and no repository path was specified in agents.yml to install it", skillName)
		}

		ui.PrintInfo(fmt.Sprintf("Downloading and installing skill '%s'...", skillName))

		// Try installing globally via npx skills
		cmd := execCommand("npx", "-y", "skills", "add", "-g", repo, "--skill", skillName)
		var cmdOutput bytes.Buffer
		cmd.Stdout = &cmdOutput
		cmd.Stderr = &cmdOutput
		cmdErr := cmd.Run()

		var fallbackOutput bytes.Buffer
		var fallbackErr error
		if cmdErr != nil {
			// Fallback to npx @vercel-labs/skills
			fallbackCmd := execCommand("npx", "-y", "@vercel-labs/skills", "add", "-g", repo, "--skill", skillName)
			fallbackCmd.Stdout = &fallbackOutput
			fallbackCmd.Stderr = &fallbackOutput
			fallbackErr = fallbackCmd.Run()
		}

		// Re-verify that installation succeeded
		if !isSkillInstalledFunc(skillName) {
			if cmdErr != nil && fallbackErr != nil {
				return fmt.Errorf("failed to install skill '%s' from repository '%s': %v\nOutput: %s\nFallback Output: %s",
					skillName, repo, cmdErr, cmdOutput.String(), fallbackOutput.String())
			}
			return fmt.Errorf("failed to verify installation of skill '%s': file still not found in any local or global skills directory.\nOutput: %s",
				skillName, cmdOutput.String())
		}

		ui.PrintSuccess(fmt.Sprintf("Skill '%s' installed successfully.", skillName))
	}
	return nil
}
