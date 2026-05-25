package skills

import (
	"os"
	"os/exec"
	"strings"
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
			if i+1 < len(args) && args[i+1] == "npx" {
				for j := i + 2; j < len(args); j++ {
					if args[j] == "invalid-skill" || args[j] == "invalid-repo" {
						os.Exit(1)
					}
				}
				os.Exit(0)
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
	origInstalledFunc := isSkillInstalledFunc
	defer func() {
		execCommand = origExec
		isSkillInstalledFunc = origInstalledFunc
	}()

	execCommand = mockExecCommand

	// Mock file existence check by tracking installed skills in a map
	installedSkills := make(map[string]bool)

	tests := []struct {
		name        string
		skills      []string
		setupMock   func()
		wantErr     bool
		expectedErr string
	}{
		{
			name:    "empty skills list should do nothing",
			skills:  []string{},
			wantErr: false,
		},
		{
			name:   "successful installation of multiple missing skills",
			skills: []string{"aws/cli-manager/aws-cli", "kubernetes/k8s-debugger/k8s-dbg"},
			setupMock: func() {
				installedSkills["aws-cli"] = false
				installedSkills["k8s-dbg"] = false
			},
			wantErr: false,
		},
		{
			name:   "skills already installed should be skipped (no command executed)",
			skills: []string{"installed-skill"},
			setupMock: func() {
				installedSkills["installed-skill"] = true
			},
			wantErr: false,
		},
		{
			name:        "fail-fast on missing skill with no repo defined",
			skills:      []string{"just-a-name-no-repo"},
			setupMock:   func() {},
			wantErr:     true,
			expectedErr: "skill 'just-a-name-no-repo' is not installed, and no repository path was specified in agents.yml to install it",
		},
		{
			name:   "fail-fast when installation command fails",
			skills: []string{"invalid-repo/invalid-skill"},
			setupMock: func() {
				installedSkills["invalid-skill"] = false
			},
			wantErr:     true,
			expectedErr: "failed to install skill 'invalid-skill' from repository 'invalid-repo/invalid-skill': exit status 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset map
			installedSkills = make(map[string]bool)
			if tt.setupMock != nil {
				tt.setupMock()
			}

			// Wrap isSkillInstalledFunc to dynamically simulate successful installation.
			isSkillInstalledFunc = func(skillName string) bool {
				if installedSkills[skillName] {
					return true
				}
				// If it's a success test case, simulate that the installation succeeded
				if !tt.wantErr && skillName != "just-a-name-no-repo" {
					installedSkills[skillName] = true
					return false // First check returns false (not installed), subsequent checks return true (installed)
				}
				return false
			}

			err := AutoEquip(tt.skills)
			if (err != nil) != tt.wantErr {
				t.Fatalf("AutoEquip() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.expectedErr) {
				t.Errorf("AutoEquip() error = %q, expected to contain %q", err.Error(), tt.expectedErr)
			}
		})
	}
}

func TestParseSkillDeclaration(t *testing.T) {
	tests := []struct {
		name         string
		decl         string
		expectedRepo string
		expectedName string
	}{
		{
			name:         "standard format",
			decl:         "vercel-labs/agent-skills/vercel-react-best-practices",
			expectedRepo: "vercel-labs/agent-skills",
			expectedName: "vercel-react-best-practices",
		},
		{
			name:         "github https url",
			decl:         "https://github.com/anthropics/skills/frontend-design",
			expectedRepo: "https://github.com/anthropics/skills",
			expectedName: "frontend-design",
		},
		{
			name:         "simple name only",
			decl:         "just-a-name",
			expectedRepo: "",
			expectedName: "just-a-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, name := parseSkillDeclaration(tt.decl)
			if repo != tt.expectedRepo {
				t.Errorf("parseSkillDeclaration() repo = %q, expected %q", repo, tt.expectedRepo)
			}
			if name != tt.expectedName {
				t.Errorf("parseSkillDeclaration() name = %q, expected %q", name, tt.expectedName)
			}
		})
	}
}
