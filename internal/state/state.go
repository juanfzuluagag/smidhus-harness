package state

import (
	"encoding/json"
	"os"
)

type Task struct {
	ID             string   `json:"id"`
	Description    string   `json:"description"`
	Status         string   `json:"status"`
	RequiredAgents []string `json:"required_agents,omitempty"`
	AgentIndex     int      `json:"agent_index,omitempty"`
	Error          string   `json:"error,omitempty"`
	FailedAttempts int      `json:"failed_attempts,omitempty"`
}

type ProjectState struct {
	Project string `json:"project"`
	Tasks   []Task `json:"tasks"`
}

func LoadState(path string) (*ProjectState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state ProjectState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func SaveState(path string, state *ProjectState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}