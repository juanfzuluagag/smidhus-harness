package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// strictIgnores holds directories that should never be scanned to prevent
// infinite loops or huge context sizes.
var strictIgnores = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
	".harness":     true,
	".next":        true,
	".nuxt":        true,
	".venv":        true,
	"venv":         true,
	"env":          true,
	"target":       true,
	"tmp":          true,
	"out":          true,
	".gradle":      true,
	".idea":        true,
	".vscode":      true,
}

// ShallowScan walks the directory structure of rootDir up to maxDepth.
// It parses the local .gitignore to avoid scanning dependency and build folders,
// returning a clean string representing the directories and root-level key files.
func ShallowScan(rootDir string, maxDepth int) (string, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	gitignorePatterns, err := parseGitignore(rootDir)
	if err != nil {
		// Log of error or fallback to empty patterns is acceptable.
		gitignorePatterns = []string{}
	}

	var sb strings.Builder
	dirCount := 0
	err = scanDir(rootDir, rootDir, 0, maxDepth, gitignorePatterns, &dirCount, &sb)
	if err != nil {
		return "", err
	}
	return sb.String(), nil
}

// parseGitignore reads the project's .gitignore file and normalizes its rules.
func parseGitignore(rootDir string) ([]string, error) {
	path := filepath.Join(rootDir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		// Do not fail if there is no gitignore file.
		return nil, nil
	}

	var patterns []string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Remove leading/trailing slashes for simple matching
		line = strings.TrimPrefix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if line != "" {
			patterns = append(patterns, line)
		}
	}
	return patterns, nil
}

// shouldIgnore checks if the relative path or file name matches any of our ignore lists.
func shouldIgnore(relPath string, name string, gitignorePatterns []string) bool {
	if strictIgnores[name] {
		return true
	}

	for _, pat := range gitignorePatterns {
		// Match exact folder/file path or check if it matches as a prefix/suffix segment
		if relPath == pat || strings.HasPrefix(relPath, pat+"/") || strings.Contains(relPath, "/"+pat+"/") || strings.HasSuffix(relPath, "/"+pat) {
			return true
		}
		// Basic wildcard support (e.g. *.log)
		if strings.Contains(pat, "*") {
			cleanPat := strings.ReplaceAll(pat, "*", "")
			if cleanPat != "" && strings.Contains(name, cleanPat) {
				return true
			}
		}
	}
	return false
}

// scanDir traverses the workspace recursively up to maxDepth.
func scanDir(rootDir, currentDir string, depth, maxDepth int, gitignorePatterns []string, dirCount *int, sb *strings.Builder) error {
	if depth > maxDepth {
		return nil
	}

	// Prevent excessive directory traversal in huge codebases
	if *dirCount > 300 {
		if *dirCount == 301 {
			sb.WriteString("[...] (directory structure truncated due to size)\n")
			*dirCount++
		}
		return nil
	}

	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return err
	}

	indent := strings.Repeat("  ", depth)

	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(currentDir, name)
		relPath, err := filepath.Rel(rootDir, fullPath)
		if err != nil {
			continue
		}

		if shouldIgnore(relPath, name, gitignorePatterns) {
			continue
		}

		if entry.IsDir() {
			*dirCount++
			sb.WriteString(fmt.Sprintf("%s|-- %s/\n", indent, name))
			err = scanDir(rootDir, fullPath, depth+1, maxDepth, gitignorePatterns, dirCount, sb)
			if err != nil {
				return err
			}
		} else {
			// Limit file output to depth 0 (project root) to avoid bloating the string tree.
			if depth == 0 {
				sb.WriteString(fmt.Sprintf("%s- %s\n", indent, name))
			}
		}
	}

	return nil
}
