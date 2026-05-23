package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShallowScan(t *testing.T) {
	// Create a temp root directory.
	tmpDir := t.TempDir()

	// Set up mock project structure.
	// Root level:
	// - main.go
	// - .gitignore
	// - app.log (should be ignored by wildcard)
	// Subdirectories:
	// - .git/ (strictly ignored)
	// - node_modules/ (strictly ignored)
	// - src/
	//   - api/ (depth 2)
	//     - handler.go (skipped because nested files are omitted, only directories are recurse-printed)
	// - temp_ignored/ (ignored by gitignore pattern)
	for _, folder := range []string{
		".git",
		"node_modules",
		"src/api",
		"temp_ignored",
	} {
		err := os.MkdirAll(filepath.Join(tmpDir, folder), 0755)
		if err != nil {
			t.Fatalf("failed to create temp folder structure: %v", err)
		}
	}

	// Create files
	err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	if err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("temp_ignored/\n*.log\n"), 0644)
	if err != nil {
		t.Fatalf("failed to write .gitignore: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "temp_ignored", "config.json"), []byte("{}"), 0644)
	if err != nil {
		t.Fatalf("failed to write config.json in ignored dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "src", "api", "handler.go"), []byte("package api"), 0644)
	if err != nil {
		t.Fatalf("failed to write nested file: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "app.log"), []byte("logs"), 0644)
	if err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}

	// Run shallow scan
	result, err := ShallowScan(tmpDir, 3)
	if err != nil {
		t.Fatalf("ShallowScan failed: %v", err)
	}

	// Assertions on the tree representation
	if !strings.Contains(result, "- main.go") {
		t.Errorf("expected tree to contain root file '- main.go', got:\n%s", result)
	}

	if !strings.Contains(result, "- .gitignore") {
		t.Errorf("expected tree to contain root file '- .gitignore', got:\n%s", result)
	}

	if strings.Contains(result, ".git/") {
		t.Errorf("expected tree to exclude strictly ignored '.git/', got:\n%s", result)
	}

	if strings.Contains(result, "node_modules/") {
		t.Errorf("expected tree to exclude strictly ignored 'node_modules/', got:\n%s", result)
	}

	if strings.Contains(result, "temp_ignored/") {
		t.Errorf("expected tree to exclude gitignored 'temp_ignored/', got:\n%s", result)
	}

	if strings.Contains(result, "- app.log") {
		t.Errorf("expected tree to exclude gitignored wildcard file '- app.log', got:\n%s", result)
	}

	if !strings.Contains(result, "|-- src/") {
		t.Errorf("expected tree to include folder '|-- src/', got:\n%s", result)
	}

	if !strings.Contains(result, "  |-- api/") {
		t.Errorf("expected tree to include nested folder '  |-- api/', got:\n%s", result)
	}

	if strings.Contains(result, "handler.go") {
		t.Errorf("expected tree to omit nested files like 'handler.go', got:\n%s", result)
	}
}

func TestSyncBlueprintTree(t *testing.T) {
	t.Run("replace markers in existing blueprint", func(t *testing.T) {
		tmpDir := t.TempDir()
		blueprintPath := filepath.Join(tmpDir, "blueprint.md")

		initialContent := `# Project Blueprint

## 1. Stack
- Go

## 2. Architecture & Directory Structure
Some description.

<!-- dir_tree_start -->
old_tree_to_be_replaced/
<!-- dir_tree_end -->

## 3. End`

		err := os.WriteFile(blueprintPath, []byte(initialContent), 0644)
		if err != nil {
			t.Fatalf("failed to write mock blueprint: %v", err)
		}

		newTree := "|-- internal/\n  |-- scanner/\n- main.go"
		err = SyncBlueprintTree(blueprintPath, newTree)
		if err != nil {
			t.Fatalf("SyncBlueprintTree failed: %v", err)
		}

		updatedBytes, err := os.ReadFile(blueprintPath)
		if err != nil {
			t.Fatalf("failed to read updated blueprint: %v", err)
		}
		updatedContent := string(updatedBytes)

		expectedBlock := "<!-- dir_tree_start -->\n|-- internal/\n  |-- scanner/\n- main.go\n<!-- dir_tree_end -->"
		if !strings.Contains(updatedContent, expectedBlock) {
			t.Errorf("tree content was not correctly replaced between markers, got:\n%s", updatedContent)
		}

		if !strings.Contains(updatedContent, "## 2. Architecture & Directory Structure") {
			t.Errorf("expected existing headings to remain intact, got:\n%s", updatedContent)
		}
	})

	t.Run("append markers to end when missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		blueprintPath := filepath.Join(tmpDir, "blueprint.md")

		initialContent := `# Project Blueprint

## 1. Stack
- Go`

		err := os.WriteFile(blueprintPath, []byte(initialContent), 0644)
		if err != nil {
			t.Fatalf("failed to write mock blueprint: %v", err)
		}

		newTree := "|-- internal/\n  |-- scanner/\n- main.go"
		err = SyncBlueprintTree(blueprintPath, newTree)
		if err != nil {
			t.Fatalf("SyncBlueprintTree failed: %v", err)
		}

		updatedBytes, err := os.ReadFile(blueprintPath)
		if err != nil {
			t.Fatalf("failed to read updated blueprint: %v", err)
		}
		updatedContent := string(updatedBytes)

		if !strings.Contains(updatedContent, "## Directory Structure") {
			t.Errorf("expected new heading '## Directory Structure' to be created, got:\n%s", updatedContent)
		}

		expectedBlock := "<!-- dir_tree_start -->\n|-- internal/\n  |-- scanner/\n- main.go\n<!-- dir_tree_end -->"
		if !strings.Contains(updatedContent, expectedBlock) {
			t.Errorf("tree content was not correctly appended with markers, got:\n%s", updatedContent)
		}
	})
}

func TestShallowScanTruncation(t *testing.T) {
	tmpDir := t.TempDir()
	// Create 305 directories
	for i := 0; i < 305; i++ {
		err := os.MkdirAll(filepath.Join(tmpDir, fmt.Sprintf("dir_%d", i)), 0755)
		if err != nil {
			t.Fatalf("failed to create folder: %v", err)
		}
	}
	result, err := ShallowScan(tmpDir, 3)
	if err != nil {
		t.Fatalf("ShallowScan failed: %v", err)
	}
	if !strings.Contains(result, "[...] (directory structure truncated") {
		t.Error("expected directory structure to be truncated and show message")
	}
}
