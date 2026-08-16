package tool

import (
	"context"
	"log"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
)

// Holds the global tool config
var cfg *config.Config = nil


// Holds all registered tools
var registry = map[string]Tool{}


// ToolHandler executes a tool with raw JSON input and returns raw JSON output.
type ToolHandler func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error)

// Tool describes a function that can be called by an AI agent.
type Tool interface {
	Name() string //name of the tool wich is used to call it and refer to it
	Description() string // description for the agent
	Parameters() map[string]any // JSON schema for OpenAI function calling
	Handler() ToolHandler //handler wich executes the tool
}



func Register(t Tool) {
	name := t.Name()
	if _, exists := registry[name]; exists {
		log.Fatalf("tool %q already registered", name)
	}
	registry[name] = t
}


func Get(name string) Tool {
	t, ok := registry[name]
	if !ok {
		log.Fatalf("unknown tool: %s", name)
	}
	return t
}

func Exists(name string) bool {
	_, ok := registry[name]
	return ok
}


func All() []Tool {
	out := make([]Tool, 0, len(registry))
	for _, t := range registry {
		out = append(out, t)
	}
	return out
}


func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
}



func LoadToolConfig(path string) error {
	conf, err := config.New(path)
	if err != nil {
		return err
	}
	cfg = conf
	return nil
}


func GetToolConfig() *config.Config {
    if cfg == nil {
        panic("tools: config not initialized.")
    }
    return cfg
}
