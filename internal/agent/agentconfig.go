package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/alex0esc/ceres/internal/constants"
	"github.com/alex0esc/ceres/internal/inference"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/openai/openai-go/v3/responses"
	"github.com/robfig/cron/v3"
)

// AgentConfig is the on-disk representation of an agent, loaded from a .toml file
type AgentConfig struct {
	Endpoint             string         `toml:"endpoint"`               // Reference to endpoint name (resolved against endpoints map)
	ModelName            string         `toml:"model_name"`            // Model identifier at the endpoint
	ExtraBody            map[string]any `toml:"extra_body,omitempty"`  // Additional API request parameters
	ReasoningSummary     bool           `toml:"reasoning_summary"`     // Use reasoning summary instead of full reasoning
	Name                 string         `toml:"name"`                  // Agent name (supports <name> placeholder in system_prompt)
	Description          string         `toml:"description"`           // Human-readable description
	ReasoningEffort      string         `toml:"reasoning_effort"`      // Reasoning effort level (none/low/medium/high)
	Quantity             int            `toml:"quantity"`              // Number of agent instances to create
	Subagent             bool           `toml:"subagent"`              // Whether this is a subagent
	MaxToolIterations    int            `toml:"max_tool_iterations"`   // Maximum tool call rounds per request
	Tools                []string       `toml:"tools"`                 // List of tool names to register
	SystemPrompt         string         `toml:"system_prompt"`         // System prompt (use <name> as placeholder)
	CompressionThreshold int64          `toml:"compression_threshold"` // Token count threshold for history compression
	NumMessagesToKeep    int            `toml:"num_messages_to_keep"`  // Number of messages to keep after compression
	CompressionPrompt    string         `toml:"compression_prompt"`    // Prompt used for history compression
}

// LoadAgentFromFile reads an agent's .toml config and wires it up with the
// matching endpoint and tools from the provided registries. If Quantity is
// greater than 1, multiple agents are returned, named "<name>-1" .. "<name>-n".
func loadAgentFromFile(path string, endpoints map[string]inference.Endpoint, cronLib *cron.Cron) ([]*Agent, error) {
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

		client := inference.NewClient(&endpoint, cfg.ModelName)
		client.ExtraBody = cfg.ExtraBody
		// ensure <name> works for all quantities
		client.SystemPrompt = strings.ReplaceAll(cfg.SystemPrompt, "<name>", name)
		client.ReasoningEffort = responses.ReasoningEffort(cfg.ReasoningEffort)
		client.MaxToolIterations = cfg.MaxToolIterations
		client.NumMessagesToKeep = cfg.NumMessagesToKeep
		client.CompressionThreshold = cfg.CompressionThreshold
		client.CompressionPrompt = cfg.CompressionPrompt
		client.UseReasoningSummary = cfg.ReasoningSummary

		// resolve each referenced tool name against the registry
		for _, toolName := range cfg.Tools {
			if !tool.Exists(toolName) {
				return nil, fmt.Errorf("agent %q references unknown tool %q", name, toolName)
			}
			client.RegisterTool(tool.Get(toolName))
		}

		agnt, err := NewAgent(name, cfg.Description, client, cfg.Subagent, cronLib)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agnt)
	}

	return agents, nil
}

// LoadAgentsFromDir reads every .toml file in the given directory and loads
// each one as an Agent (or several, if quantity > 1), wiring it up against
// the provided endpoints.
func LoadAgentsFromDir(endpoints map[string]inference.Endpoint, cronLib *cron.Cron) (map[string]*Agent, error) {
	dir := constants.AgentsFolderPath
	err := ensureOneAgentFile(dir)
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
		loaded, err := loadAgentFromFile(path, endpoints, cronLib)
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
func ensureOneAgentFile(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read agents directory: %w", err)
	}
	// if any .toml file exists, nothing to do
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".toml") {
			return nil
		}
	}
	// no agent file found -> create default agent

	cfg := AgentConfig{
		Name:                 "Ceres",
		ModelName:            "qwen3.8",
		ExtraBody:            nil,
		ReasoningSummary:     false,
		Description:          "The main agent of the system.",
		ReasoningEffort:      string(responses.ReasoningEffortMedium),
		SystemPrompt:         "You are <name>, a helpful AI assistant.",
		Tools:                tool.Names(),
		Endpoint:             "llama",
		Quantity:             1,
		Subagent:             false,
		MaxToolIterations:    30,
		NumMessagesToKeep:    8,
		CompressionThreshold: 200000,
		CompressionPrompt:    "Your task is to summarize the current chat. Make it precise and don't leave anything important out.",
	}
	path := filepath.Join(dir, "ceres.toml")
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create default agent file: %w", err)
	}
	defer file.Close()

	// Write a header comment explaining the agent config
	header := `# Agent Configuration File
#
# This file defines an AI agent and its behavior.
# You can create multiple agent files to run different agents.
#
# Endpoint: Reference to an endpoint defined in your endpoints config
# Model Name: The model identifier at the endpoint (e.g. "qwen3.8", "gpt-4")
# Name: Agent name (use <name> in system_prompt as placeholder)
# Description: Human-readable description of the agent
# Reasoning Effort: Reasoning effort level (none/low/medium/high)
# Reasoning Summary: Use summary instead of full reasoning output
# System Prompt: The agent's system prompt (use <name> as placeholder for agent name)
# Tools: List of tool names the agent can use
# Max Tool Iterations: Maximum number of tool call rounds per request
# Compression Threshold: Token count that triggers history compression
# Num Messages To Keep: Messages to keep after compression
# Compression Prompt: Prompt used when compressing history
# Quantity: Number of agent instances to create (creates <name>-1, <name>-2, etc.)
# Subagent: Whether this agent is a subagent
# Extra Body: Additional API parameters (advanced)
#
`
	if _, err := file.WriteString(header); err != nil {
		return fmt.Errorf("failed to write agent config header: %w", err)
	}

	if err := toml.NewEncoder(file).Encode(cfg); err != nil {
		return fmt.Errorf("failed to write default agent file: %w", err)
	}
	return nil
}
