package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// ExecuteCodeTool runs a short Python snippet inside the sandbox container
// and returns its stdout/stderr/exit code. Intended for quick, self-contained
// tasks (calculations, data wrangling, etc.), not long-running scripts.
type ExecuteCodeTool struct {
	timeout time.Duration
}

// NewExecuteCodeTool constructs an ExecuteCodeTool, reading all relevant
// config values once up front.
func NewExecuteCodeTool() ExecuteCodeTool {
	cfg := tool.GetToolConfig()

	timeout := config.ReadEntry(cfg, "execute_code.timeout", time.Second*180)

	return ExecuteCodeTool{
		timeout: timeout,
	}
}

func (ExecuteCodeTool) Name() string {
	return "execute_code"
}

func (ExecuteCodeTool) Description() string {
	return "Executes a short Python 3 code snippet inside the sandbox container and returns stdout, stderr, and the exit code. " +
		"Use this for on-demand calculations, quick data processing, or logic checks — not for long-running or interactive scripts. " +
		"The snippet has no persistent state between calls (each execution is a fresh process); print() whatever result you need to see."
}

func (ExecuteCodeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{
				"type":        "string",
				"description": "The Python 3 source code to execute. Use print() to output any values you need to see the result of.",
			},
		},
		"required":             []string{"code"},
		"additionalProperties": false,
	}
}

func (t ExecuteCodeTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("execute_code: invalid arguments: %w", err)
		}
		if strings.TrimSpace(args.Code) == "" {
			return "", fmt.Errorf("execute_code: code must not be empty")
		}

		// Pipe the code through base64 to avoid any shell-quoting issues with
		// the snippet's own content (quotes, $, backticks, newlines, ...).
		// No temp file is written to disk — python3 reads the script from stdin.
		encoded := base64.StdEncoding.EncodeToString([]byte(args.Code))
		runCmd := fmt.Sprintf("echo %s | base64 -d | python3 -", shellQuote(encoded))

		execCtx, cancel := context.WithTimeout(ctx, t.timeout+2*killAfterBuffer)
		defer cancel()
		stdout, stderr, exitCode, err := runInContainer(execCtx, handle.SandboxContainer(), runCmd)
		if err != nil {
			return "", fmt.Errorf("execute_code: failed to execute code in sandbox: %w", err)
		}

		out := struct {
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			ExitCode int    `json:"exit_code"`
		}{
			Stdout:   stdout,
			Stderr:   stderr,
			ExitCode: exitCode,
		}
		result, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("execute_code: failed to marshal result: %w", err)
		}
		return string(result), nil
	}
}
