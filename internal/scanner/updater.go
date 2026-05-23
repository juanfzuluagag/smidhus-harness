package scanner

import (
	"fmt"
	"os"
	"strings"
)

const (
	startMarker = "<!-- dir_tree_start -->"
	endMarker   = "<!-- dir_tree_end -->"
)

// SyncBlueprintTree reads blueprintPath, locates the start and end HTML markers,
// and replaces all content between them with the new treeContent. If the markers
// are missing, it appends a new '## Directory Structure' section to the end.
func SyncBlueprintTree(blueprintPath string, treeContent string) error {
	data, err := os.ReadFile(blueprintPath)
	if err != nil {
		return fmt.Errorf("could not read blueprint file: %w", err)
	}

	content := string(data)
	startIdx := strings.Index(content, startMarker)
	endIdx := strings.Index(content, endMarker)

	var newContent string

	// Ensure markers exist and are ordered correctly.
	if startIdx != -1 && endIdx != -1 && startIdx < endIdx {
		before := content[:startIdx+len(startMarker)]
		after := content[endIdx:]
		newContent = before + "\n" + strings.TrimSpace(treeContent) + "\n" + after
	} else {
		// Markers were missing; append them cleanly at the bottom.
		trimmed := strings.TrimSpace(content)
		var sb strings.Builder
		sb.WriteString(trimmed)
		sb.WriteString("\n\n## Directory Structure\n")
		sb.WriteString(startMarker)
		sb.WriteString("\n")
		sb.WriteString(strings.TrimSpace(treeContent))
		sb.WriteString("\n")
		sb.WriteString(endMarker)
		sb.WriteString("\n")
		newContent = sb.String()
	}

	err = os.WriteFile(blueprintPath, []byte(newContent), 0644)
	if err != nil {
		return fmt.Errorf("could not write updated blueprint: %w", err)
	}

	return nil
}
