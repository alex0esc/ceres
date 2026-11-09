package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/alex0esc/ceres/internal/constants"
	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/inference"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/openai/openai-go/v3/responses"
	"github.com/robfig/cron/v3"
)

// loadAgentFromFile reads an agent's .toml config and wires it up with the
// matching endpoint and tools from the provided registries. The agent is named
// after its config file: Ceres.toml defines the agent "Ceres". If Quantity is
// greater than 1, multiple agents are returned, named "<name>-1" .. "<name>-n".
func loadAgentFromFile(path string, endpoints map[string]inference.Endpoint, cronLib *cron.Cron) ([]*Agent, error) {
	cfg, err := config.New(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load agent config %q: %w", path, err)
	}

	// the agent is named after its config file (without the extension)
	baseName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	// resolve endpoint by name
	endpointName := config.ReadEntry(cfg, "endpoint", "llama")
	endpoint, ok := endpoints[endpointName]
	if !ok {
		return nil, fmt.Errorf("agent %q references unknown endpoint %q", baseName, endpointName)
	}

	description := config.ReadEntry(cfg, "description", "The main agent of the system.")
	sandboxContainer := config.ReadEntry(cfg, "sandbox_container_name", "ceres-sandbox")
	subagent := config.ReadEntry(cfg, "subagent", false)
	modelName := config.ReadEntry(cfg, "model_name", "qwen3.8")
	reasoningEffort := config.ReadEntry(cfg, "reasoning_effort", string(responses.ReasoningEffortMedium))
	reasoningSummary := config.ReadEntry(cfg, "reasoning_summary", false)
	maxToolIterations := config.ReadEntry(cfg, "max_tool_iterations", 100)
	maxToolResultChars := config.ReadEntry(cfg, "max_tool_result_chars", 100000)
	numMessagesToKeep := config.ReadEntry(cfg, "num_messages_to_keep", 8)
	compressionThreshold := config.ReadEntry(cfg, "compression_threshold", int64(200000))
	compressionPrompt := config.ReadEntry(cfg, "compression_prompt", "This is a message injected by the system, your task is to compress the current chat.\n" +
		"Dont ask any questions, do the summary in this turn and keep everything that is relevant.")
	systemPrompt := config.ReadEntry(cfg, "system_prompt", "You are <name>, a helpful AI assistant.")
	enabledTools := config.ReadEntry(cfg, "enabled_tools", tool.Names())
	quantity := max(config.ReadEntry(cfg, "quantity", 1), 1)

	// extra_body is an optional JSON object with additional API parameters.
	var extraBody map[string]any
	if raw := config.ReadEntry(cfg, "extra_body", ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &extraBody); err != nil {
			return nil, fmt.Errorf("agent %q has an invalid extra_body JSON object: %w", baseName, err)
		}
	}

	agents := make([]*Agent, 0, quantity)
	for i := range quantity {
		name := baseName
		if quantity > 1 {
			name = fmt.Sprintf("%s-%d", baseName, i+1)
		}

		client := inference.NewClient(&endpoint, modelName)
		client.ExtraBody = extraBody
		// ensure <name> works for all quantities
		client.SystemPrompt = strings.ReplaceAll(systemPrompt, "<name>", name)
		client.ReasoningEffort = responses.ReasoningEffort(reasoningEffort)
		client.MaxToolIterations = maxToolIterations
		client.MaxToolResultChars = maxToolResultChars
		client.NumMessagesToKeep = numMessagesToKeep
		client.CompressionThreshold = compressionThreshold
		client.CompressionPrompt = compressionPrompt
		client.UseReasoningSummary = reasoningSummary

		// resolve each referenced tool name against the registry
		for _, toolName := range enabledTools {
			if !tool.Exists(toolName) {
				return nil, fmt.Errorf("agent %q references unknown tool %q", name, toolName)
			}
			client.RegisterTool(tool.Get(toolName))
		}

		// restore the agent's persisted chat history; a failure must not stop the
		// agent from starting, it just begins with an empty history
		loadedHistory, err := history.StorageLoad(name)
		if err != nil {
			slog.Warn("failed to load persisted history, starting with empty history", "agent", name, "error", err)
			loadedHistory = history.History{}
		}
		client.History = loadedHistory

		agnt, err := NewAgent(name, description, sandboxContainer, client, subagent, cfg, cronLib)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agnt)
	}

	return agents, nil
}

// LoadAgentsFromDir reads every .toml file in the agents directory and loads
// each one as an Agent (or several, if quantity > 1), wiring it up against the
// provided endpoints. If no agent file exists yet, a default one is created by
// loading it: config.ReadEntry writes all missing keys.
func LoadAgentsFromDir(endpoints map[string]inference.Endpoint, cronLib *cron.Cron) (map[string]*Agent, error) {
	dir := constants.AgentsFolderPath
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create agents directory: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read agents directory %q: %w", dir, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".toml") {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	if len(paths) == 0 {
		paths = append(paths, filepath.Join(dir, "Ceres.toml"))
	}

	agents := make(map[string]*Agent, len(paths))
	for _, path := range paths {
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
