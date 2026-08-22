package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpToolAdapter implements your tool.Tool interface for any MCP tool.
type mcpToolAdapter struct {
	name        string
	description string
	parameters  map[string]any
	handler     ToolHandler
}

func (m *mcpToolAdapter) Name() string               { return m.name }
func (m *mcpToolAdapter) Description() string        { return m.description }
func (m *mcpToolAdapter) Parameters() map[string]any { return m.parameters }
func (m *mcpToolAdapter) Handler() ToolHandler       { return m.handler }

// RegisterAlpacaMCPToolsOfficial registers the Alpaca MCP Server 
// directly into your registry using the official SDK.
func registerAlpacaMcpTools(ctx context.Context) error {

	paperStr := "true"
	if !config.ReadEntry(GetToolConfig(), "alpaca.paper_trade", true) {
		paperStr = "false"
	}

	// 1. Define official MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "ceres-agent",
		Version: "1.0.0",
	}, nil)

	// 2. Set up CommandTransport with environment variables
	cmd := exec.Command("uvx", "alpaca-mcp-server", "serve")
	cmd.Env = append(os.Environ(),
		"ALPACA_API_KEY=" + config.ReadEntry(GetToolConfig(), "alpaca.alpaca_api_key", "<my_api_key>"),
		"ALPACA_SECRET_KEY=" + config.ReadEntry(GetToolConfig(), "alpaca.alpaca_secret_key", "<my_secret_key>"),
		"ALPACA_PAPER_TRADE=" + paperStr,
	)

	transport := &mcp.CommandTransport{Command: cmd}

	// 3. Establish connection (creates the ClientSession)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to alpaca mcp server: %w", err)
	}

	// 4. Query tools from the server
	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to list mcp tools: %w", err)
	}

	// 5. Iterate through tools and register adapter instances
	for _, t := range toolsResult.Tools {
		mcpToolName := t.Name

		var paramsMap map[string]any
		if t.InputSchema != nil {
			if schemaBytes, err := json.Marshal(t.InputSchema); err == nil {
				_ = json.Unmarshal(schemaBytes, &paramsMap)
			}
		}

		Register(&mcpToolAdapter{
			name:        mcpToolName,
			description: t.Description,
			parameters:  paramsMap,
			handler: func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
				var args map[string]any
				if argumentsJSON != "" && argumentsJSON != "{}" {
					if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
						return "", fmt.Errorf("invalid json arguments for tool %s: %w", mcpToolName, err)
					}
				}

				// Execute call via the official ClientSession
				callParams := &mcp.CallToolParams{
					Name:      mcpToolName,
					Arguments: args,
				}

				res, err := session.CallTool(ctx, callParams)
				if err != nil {
					return "", fmt.Errorf("mcp tool execution failed: %w", err)
				}

				resBytes, err := json.Marshal(res.Content)
				if err != nil {
					return "", fmt.Errorf("failed to marshal mcp response: %w", err)
				}
				return string(resBytes), nil
			},
		})
	}

	return nil
}
