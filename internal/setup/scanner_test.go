package setup

import (
	"os"
	"path/filepath"
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

