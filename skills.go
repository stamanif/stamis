package main

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SkillDef strictly maps to the OpenAI tool function specification.
type SkillDef struct {
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Command     string                 `yaml:"command" json:"-"`
	Parameters  map[string]interface{} `yaml:"parameters" json:"parameters"`
}

// Tool acts as the standard payload envelope.
type Tool struct {
	Type     string   `json:"type"`
	Function SkillDef `json:"function"`
}

// ActiveSkills acts as our live, in-memory command registry.
var ActiveSkills map[string]SkillDef

// LoadSkills scans the dynamic /skills directory natively and builds the registry.
func LoadSkills(dir string) []Tool {
	var tools []Tool
	ActiveSkills = make(map[string]SkillDef)

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Suppress error if the skills directory doesn't exist yet
		return tools
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".yaml" {
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}

			var def SkillDef
			if err := yaml.Unmarshal(data, &def); err == nil {
				tools = append(tools, Tool{
					Type:     "function",
					Function: def,
				})
				ActiveSkills[def.Name] = def
			}
		}
	}

	return tools
}
