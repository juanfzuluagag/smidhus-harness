package skills

import (
	"os"
	"os/exec"
	"testing"
)

// TestHelperProcess acts as a mock executable for the exec.Command invocation.
// It detects the GO_WANT_HELPER_PROCESS flag, analyzes the intercepted command
// arguments, and exits with a mock status code depending on the skill name.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			if i+1 < len(args) && args[i+1] == "opencode" {
				if i+2 < len(args) && args[i+2] == "plugin" {
					if i+3 < len(args) {
						skill := args[i+3]
						// Simulate an installation failure for a designated invalid skill name.
						if skill == "invalid-skill" {
							os.Exit(1)
						}
						os.Exit(0)
					}
				}
			}
		}
	}
	os.Exit(0)
}

// mockExecCommand replaces the standard exec.Command to invoke this test binary
// as a helper process instead of calling out to the system shell.
func mockExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return cmd
}

func TestAutoEquip(t *testing.T) {
	// Preserve the original command exec function and restore it afterwards.
	origExec := execCommand
	defer func() { execCommand = origExec }()

	execCommand = mockExecCommand

	tests := []struct {
		name        string
		skills      []string
		wantErr     bool
		expectedErr string
	}{
		{
			name:    "empty skills list should do nothing",
			skills:  []string{},
			wantErr: false,
		},
		{
			name:    "successful installation of multiple skills",
			skills:  []string{"aws/cli-manager", "kubernetes/k8s-debugger"},
			wantErr: false,
		},
		{
			name:        "fail-fast on the first invalid skill",
			skills:      []string{"aws/cli-manager", "invalid-skill", "another-skill"},
			wantErr:     true,
			expectedErr: "Failed to equip skill 'invalid-skill'. Please verify the name in agents.yml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AutoEquip(tt.skills)
			if (err != nil) != tt.wantErr {
				t.Fatalf("AutoEquip() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err.Error() != tt.expectedErr {
				t.Errorf("AutoEquip() error = %q, want %q", err.Error(), tt.expectedErr)
			}
		})
	}
}
