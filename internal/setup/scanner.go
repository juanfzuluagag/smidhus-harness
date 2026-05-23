package setup

import (
	"os"
)

// ---------------------------------------------------------------------------
// noiseFiles are entries skipped when deciding whether a project is "blank".
// Keeping this a simple map makes the lookup O(1) without a dependency.
var noiseFiles = map[string]bool{
	".git": true, ".DS_Store": true, ".gitignore": true,
	".idea": true, ".vscode": true, "README.md": true, "readme.md": true,
	".harness": true, "node_modules": true,
}

// ---------------------------------------------------------------------------
// isBlankProject returns true when targetPath contains only noise entries.
// Used to decide between the questionnaire flow and AI-scan flow.
func isBlankProject(targetPath string) (bool, error) {
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !noiseFiles[e.Name()] {
			return false, nil
		}
	}
	return true, nil
}

