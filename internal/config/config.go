package config

import (
	"os"
	"gopkg.in/yaml.v3"
)

type AgentConfig struct {
	Model          string `yaml:"model"`
	Temperature    float32 `yaml:"temperature"`
	SpecificSkills string `yaml:"specific_skills,omitempty"`
}

type HarnessConfig struct {
	GlobalSettings map[string]interface{}  `yaml:"global_settings"`
	Agents         map[string]AgentConfig  `yaml:"agents"`
}

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