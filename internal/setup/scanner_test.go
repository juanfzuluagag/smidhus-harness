package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsBlankProject(t *testing.T) {
	t.Run("Empty directory", func(t *testing.T) {
		dir := t.TempDir()
		blank, err := isBlankProject(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !blank {
			t.Error("expected empty directory to be classified as blank")
		}
	})

	t.Run("Directory with only noise files", func(t *testing.T) {
		dir := t.TempDir()
		// create noise files
		for file := range noiseFiles {
			// skip folder directories for simple file creation
			if file == ".harness" || file == "node_modules" || file == ".git" {
				continue
			}
			err := os.WriteFile(filepath.Join(dir, file), []byte("noise content"), 0644)
			if err != nil {
				t.Fatalf("failed to create noise file: %v", err)
			}
		}

		blank, err := isBlankProject(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !blank {
			t.Error("expected directory with only noise files to be blank")
		}
	})

	t.Run("Directory with code files", func(t *testing.T) {
		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
		if err != nil {
			t.Fatalf("failed to create code file: %v", err)
		}

		blank, err := isBlankProject(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if blank {
			t.Error("expected directory with code files not to be blank")
		}
	})

	t.Run("Non-existent directory error", func(t *testing.T) {
		_, err := isBlankProject("/nonexistent/directory/path/here")
		if err == nil {
			t.Error("expected error for non-existent directory, got nil")
		}
	})
}

func TestBuildTreeAndCollectRepoXray(t *testing.T) {
	dir := t.TempDir()

	// Create directories
	dirs := []string{
		filepath.Join(dir, "src"),
		filepath.Join(dir, "node_modules"), // Should be ignored
		filepath.Join(dir, "infra"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", d, err)
		}
	}

	// Create files
	files := map[string]string{
		filepath.Join(dir, "go.mod"):               "module myapp\ngo 1.21",
		filepath.Join(dir, "src", "main.go"):       "package main",
		filepath.Join(dir, "node_modules", "a.js"): "console.log(1)",
		filepath.Join(dir, "infra", "main.tf"):     "resource aws_s3_bucket {}",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write file %s: %v", path, err)
		}
	}

	t.Run("BuildTree excludes ignored folders", func(t *testing.T) {
		tree := buildTree(dir, 0, 3)
		if strings.Contains(tree, "node_modules") {
			t.Error("buildTree should have excluded node_modules directory")
		}
		if !strings.Contains(tree, "src") {
			t.Error("buildTree should have included src directory")
		}
		if !strings.Contains(tree, "main.go") {
			t.Error("buildTree should have included src/main.go")
		}
	})

	t.Run("CollectRepoXray builds structure and collects manifests", func(t *testing.T) {
		xray := collectRepoXray(dir)
		if !strings.Contains(xray, "go.mod") {
			t.Error("xray should list go.mod manifest file")
		}
		if !strings.Contains(xray, "module myapp") {
			t.Error("xray should contain contents of go.mod")
		}
		if strings.Contains(xray, "node_modules") {
			t.Error("xray tree should not contain node_modules")
		}
	})
}
