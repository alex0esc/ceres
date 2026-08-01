package agent


import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/alex0esc/ceres/internal/openai"
	"github.com/alex0esc/ceres/pkg/tools"
	"github.com/openai/openai-go/v3/responses"
)

// AgentConfig is the on-disk representation of an agent, loaded from a
type AgentConfig struct {
	Endpoint        string   `toml:"endpoint"`
	ModelName       string   `toml:"model_name"`
	Name            string   `toml:"name"`
	Description     string   `toml:"description"`
	ReasoningEffort string   `toml:"reasoning_effort"`
	Quantity        int      `toml:"quantity"`
	Subagent        bool     `toml:"subagent"`
	Tools           []string `toml:"tools"`
	SystemPrompt    string   `toml:"system_prompt"`
}

// LoadAgentFromFile reads an agent's .toml config and wires it up with the
// matching endpoint and tools from the provided registries. If Quantity is
// greater than 1, multiple agents are returned, named "<name>-1" .. "<name>-n".
func LoadAgentFromFile(path string, endpoints map[string]openai.Endpoint) ([]*Agent, error) {
	var cfg AgentConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode agent config %q: %w", path, err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("invalid agent config %q: missing name", path)
	}
	// resolve endpoint by name
	endpoint, ok := endpoints[cfg.Endpoint]
	if !ok {
		return nil, fmt.Errorf("agent %q references unknown endpoint %q", cfg.Name, cfg.Endpoint)
	}

	quantity := max(cfg.Quantity, 1)

	agents := make([]*Agent, 0, quantity)
	for i := range quantity {
		name := cfg.Name
		if quantity > 1 {
			name = fmt.Sprintf("%s-%d", cfg.Name, i+1)
		}

		client := openai.NewClient(&endpoint, cfg.ModelName)
		//ensure <name> works for all quantities
		client.SystemPrompt = strings.ReplaceAll(cfg.SystemPrompt, "<name>", name)
		client.ReasoningEffort = responses.ReasoningEffort(cfg.ReasoningEffort)
		client.Tools = make(map[string]tools.Tool)

		// resolve each referenced tool name against the registry
		for _, toolName := range cfg.Tools {
			if !tools.Exists(toolName) {
				return nil, fmt.Errorf("agent %q references unknown tool %q", name, toolName)
			}
			client.RegisterTool(tools.Get(toolName))
		}

		agents = append(agents, NewAgent(name, cfg.Description, client, cfg.Subagent))
	}

	return agents, nil
}

// LoadAgentsFromDir reads every .toml file in the given directory and loads
// each one as an Agent (or several, if quantity > 1), wiring it up against
// the provided endpoints.
func LoadAgentsFromDir(dir string, endpoints map[string]openai.Endpoint) (map[string]*Agent, error) {
	err := EnsureOneAgentFile(dir)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read agents directory %q: %w", dir, err)
	}
	agents := make(map[string]*Agent, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".toml") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		loaded, err := LoadAgentFromFile(path, endpoints)
		if err != nil {
			return nil, fmt.Errorf("failed to load agent from %q: %w", path, err)
		}
		for _, a := range loaded {
			agents[a.Name()] = a
		}
	}
	return agents, nil
}

// CreateDefaultAgentFile creates a main agent configuration file if does not exist
func EnsureOneAgentFile(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read agents directory: %w", err)
	}
	// wenn irgendeine .toml-Datei existiert, ist nichts zu tun
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".toml") {
			return nil
		}
	}
	// keine Agent-Datei gefunden -> Default-Agent anlegen

	
	cfg := AgentConfig{
		Name:            "Ceres",
		ModelName:       "ornith:33b",
		Description:     "The main agent of the system.",
		ReasoningEffort: string(responses.ReasoningEffortMedium),
		SystemPrompt:    "You are <name> a helpful AI assistant.",
		Tools:           tools.Names(),
		Endpoint:        "ollama",
		Quantity:        1,
		Subagent:        false,
	}
	path := filepath.Join(dir, "ceres.toml")
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create default agent file: %w", err)
	}
	defer file.Close()
	if err := toml.NewEncoder(file).Encode(cfg); err != nil {
		return fmt.Errorf("failed to write default agent file: %w", err)
	}
	return nil
}
