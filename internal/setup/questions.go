package setup

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// fetchAvailableModels runs `opencode models` and groups the results by provider.
// Parsing is lenient: unknown formats fall back to the "other" bucket.
func fetchAvailableModels() (map[string][]string, error) {
	out, err := exec.Command("opencode", "models").Output()
	if err != nil {
		return nil, fmt.Errorf("could not run 'opencode models': %w", err)
	}

	modelMap := make(map[string][]string)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Clean up numerical prefixes like "[1] "
		if strings.HasPrefix(line, "[") {
			idx := strings.Index(line, "] ")
			if idx != -1 {
				line = strings.TrimSpace(line[idx+2:])
			}
		}

		parts := strings.SplitN(line, "/", 2)
		var provider, modelName string
		if len(parts) == 2 {
			provider = parts[0]
			modelName = line // keep full string like google/gemini-...
		} else {
			provider = "other"
			modelName = line
		}
		modelMap[provider] = append(modelMap[provider], modelName)
	}
	return modelMap, nil
}
