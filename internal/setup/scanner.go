package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Noise / ignore lists
// ---------------------------------------------------------------------------

var noiseFiles = map[string]bool{
	".git": true, ".DS_Store": true, ".gitignore": true,
	".idea": true, ".vscode": true, "README.md": true, "readme.md": true,
	".harness": true, "node_modules": true,
}

var ignoreForTree = map[string]bool{
	"node_modules": true, ".git": true, "bin": true, "dist": true,
	"build": true, ".harness": true, "__pycache__": true, ".next": true,
	"vendor": true, "target": true,
}

var manifestFiles = []string{
	"package.json", "go.mod", "pom.xml", "Cargo.toml",
	"pyproject.toml", "requirements.txt", "composer.json",
}

const maxManifestBytes = 4096

// ---------------------------------------------------------------------------
// Blank-project detection
// ---------------------------------------------------------------------------

// isBlankProject returns true if targetPath contains only noise files/dirs.
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

// ---------------------------------------------------------------------------
// Directory tree + manifest collection
// ---------------------------------------------------------------------------

func buildTree(root string, depth, maxDepth int) string {
	if depth > maxDepth {
		return ""
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	indent := strings.Repeat("  ", depth)
	for _, e := range entries {
		if ignoreForTree[e.Name()] {
			continue
		}
		if e.IsDir() {
			sb.WriteString(fmt.Sprintf("%s📁 %s/\n", indent, e.Name()))
			sb.WriteString(buildTree(filepath.Join(root, e.Name()), depth+1, maxDepth))
		} else {
			sb.WriteString(fmt.Sprintf("%s📄 %s\n", indent, e.Name()))
		}
	}
	return sb.String()
}

func collectManifests(targetPath string) string {
	var sb strings.Builder
	for _, name := range manifestFiles {
		path := filepath.Join(targetPath, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > maxManifestBytes {
			data = data[:maxManifestBytes]
		}
		sb.WriteString(fmt.Sprintf("\n### %s\n```\n%s\n```\n", name, string(data)))
	}
	return sb.String()
}

// collectRepoXray returns a combined directory tree + key manifest snapshot
// of targetPath, suitable for passing as context to the AI.
func collectRepoXray(targetPath string) string {
	tree := buildTree(targetPath, 0, 3)
	manifests := collectManifests(targetPath)
	return fmt.Sprintf("## Directory Structure\n```\n%s```\n## Key Manifest Files\n%s", tree, manifests)
}
