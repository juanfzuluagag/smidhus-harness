package skills

import (
	"fmt"
	"os/exec"
)

// execCommand is a package-level variable allowing unit tests to override
// the command execution behavior with a mocked process.
var execCommand = exec.Command

// AutoEquip installs a list of dynamic skills using the native OpenCode
// package installer. If any skill fails to install, execution terminates immediately.
func AutoEquip(skills []string) error {
	for _, skill := range skills {
		cmd := execCommand("opencode", "plugin", skill)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("Failed to equip skill '%s'. Please verify the name in agents.yml", skill)
		}
	}
	return nil
}
