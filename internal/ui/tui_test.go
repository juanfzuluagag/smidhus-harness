package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTUIModelUpdate(t *testing.T) {
	dummyCmd := func() tea.Msg {
		return RunStartedMsg{}
	}

	tests := []struct {
		name       string
		isFinished bool
		msg        tea.Msg
		expectQuit bool
		expectCmd  bool
	}{
		{
			name:       "Press 'r' when not finished",
			isFinished: false,
			msg:        tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")},
			expectQuit: false,
			expectCmd:  false,
		},
		{
			name:       "Press 'r' when finished",
			isFinished: true,
			msg:        tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")},
			expectQuit: false,
			expectCmd:  true,
		},
		{
			name:       "Press 'q' to quit",
			isFinished: false,
			msg:        tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")},
			expectQuit: true,
			expectCmd:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				isFinished: tt.isFinished,
				RunCmd:     dummyCmd,
			}
			m.ready = true

			_, cmd := m.Update(tt.msg)

			if tt.expectQuit {
				if cmd == nil {
					t.Fatalf("Expected a quit command, got nil")
				}
				res := cmd()
				if _, ok := res.(tea.QuitMsg); !ok {
					t.Errorf("Expected tea.QuitMsg, got %T", res)
				}
			}

			if tt.expectCmd {
				if cmd == nil {
					t.Fatalf("Expected RunCmd, got nil")
				}
				res := cmd()
				if _, ok := res.(RunStartedMsg); !ok {
					t.Errorf("Expected RunStartedMsg, got %T", res)
				}
			} else if !tt.expectQuit {
				if cmd != nil {
					res := cmd()
					if _, ok := res.(RunStartedMsg); ok {
						t.Errorf("Expected run command NOT to be triggered, but it was")
					}
				}
			}
		})
	}
}
