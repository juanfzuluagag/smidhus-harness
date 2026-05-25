package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	cmd := exec.Command("npx", "-y", "skills", "add", "-g", "vercel-labs/agent-browser", "--skill", "agent-browser")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	fmt.Printf("Error: %v\n", err)
	fmt.Printf("Stdout:\n%s\n", stdout.String())
	fmt.Printf("Stderr:\n%s\n", stderr.String())

	home, _ := os.UserHomeDir()
	fmt.Printf("Home directory in Go: %s\n", home)
}
