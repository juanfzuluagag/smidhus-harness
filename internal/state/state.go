package state

import (
	"encoding/json"
	"os"
)

type Task struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Status         string   `json:"status"`
	RequiredAgents []string `json:"required_agents,omitempty"`
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