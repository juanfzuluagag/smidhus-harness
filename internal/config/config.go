package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultTimeoutSec is used when agents.yml has no timeout set or during init
// (before agents.yml exists). 5 minutes is a safe ceiling for most models.
const DefaultTimeoutSec = 300

// DefaultThinkingBudgetMs controls how many milliseconds the thinking block
// may consume. 0 means "no explicit budget" (model decides).
const DefaultThinkingBudgetMs = 0

// GlobalSettings holds project-wide harness settings from agents.yml.
type GlobalSettings struct {
	// TimeoutSec is the maximum number of seconds to wait for any single
	// OpenCode invocation before the harness cancels it.
	TimeoutSec int `yaml:"timeout"`

	// ThinkingBudgetMs is an optional budget (in milliseconds) for the model's
	// extended-thinking phase. Set to 0 to disable the explicit budget.
	ThinkingBudgetMs int `yaml:"thinking_budget_ms"`
}

// Timeout returns the GlobalSettings timeout as a time.Duration.
// Falls back to DefaultTimeoutSec when the YAML value is 0 or missing.
func (g GlobalSettings) Timeout() time.Duration {
	secs := g.TimeoutSec
	if secs <= 0 {
		secs = DefaultTimeoutSec
	}
	return time.Duration(secs) * time.Second
}

// AgentConfig holds per-agent settings.
type AgentConfig struct {
	Model          string  `yaml:"model"`
	Temperature    float32 `yaml:"temperature"`
	SpecificSkills string  `yaml:"specific_skills,omitempty"`
}

// HarnessConfig is the top-level structure parsed from .harness/agents.yml.
type HarnessConfig struct {
	GlobalSettings GlobalSettings       `yaml:"global_settings"`
	Agents         map[string]AgentConfig `yaml:"agents"`
}

// LoadConfig reads and parses the YAML file at the given path.
func LoadConfig(path string) (*HarnessConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg HarnessConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}