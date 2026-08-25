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
type ExecuteCodeTool struct{}

func (ExecuteCodeTool) Name() string {
	return "execute_code"
}

func (ExecuteCodeTool) Description() string {
	var defMaxOutput int64 = 1024 * 20
	maxOutput := config.ReadEntry(tool.GetToolConfig(), "execute_code.max_output_b", defMaxOutput)
	return fmt.Sprintf(
		"Executes a short Python 3 code snippet inside the sandbox container and returns stdout, stderr, and the exit code. "+
			"Use this for on-demand calculations, quick data processing, or logic checks — not for long-running or interactive scripts. "+
			"The snippet has no persistent state between calls (each execution is a fresh process); print() whatever result you need to see. "+
			"Combined stdout+stderr output larger than %d bytes will be truncated.",
		maxOutput,
	)
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


func (ExecuteCodeTool) Handler() tool.ToolHandler {
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

		containerName := config.ReadEntry(tool.GetToolConfig(), "sandbox.container_name", "ceres-sandbox")
		timeout, err := time.ParseDuration(config.ReadEntry(tool.GetToolConfig(), "execute_code.timeout", "30s"))
		if err != nil {
			return "", fmt.Errorf("execute_code: error while parsing execute_code.timeout in toolconfig.toml")
		}
		if timeout <= 0 {
			return "", fmt.Errorf("execute_code: execute_code.timeout must be positive")
		}

		var defMaxOutput int64 = 1024 * 20
		maxOutput := config.ReadEntry(tool.GetToolConfig(), "execute_code.max_output_b", defMaxOutput)

		cli := getDockerClient()

		// Pipe the code through base64 to avoid any shell-quoting issues with
		// the snippet's own content (quotes, $, backticks, newlines, ...).
		// No temp file is written to disk — python3 reads the script from stdin.
		encoded := base64.StdEncoding.EncodeToString([]byte(args.Code))
		runCmd := fmt.Sprintf("echo %s | base64 -d | python3 -", shellQuote(encoded))

		execCtx, cancel := context.WithTimeout(ctx, timeout+2*killAfterBuffer)
		defer cancel()
		stdout, stderr, exitCode, err := runInContainer(execCtx, cli, containerName, runCmd)
		if err != nil {
			return "", fmt.Errorf("execute_code: failed to execute code in sandbox: %w", err)
		}

		stdout = truncateOutput(stdout, maxOutput)
		stderr = truncateOutput(stderr, maxOutput)

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


func truncateOutput(s string, maxBytes int64) string {
	if maxBytes <= 0 || int64(len(s)) <= maxBytes {
		return s
	}
	suffix := "\n... [truncated]"
	cut := max(maxBytes - int64(len(suffix)), 0)
	return s[:cut] + suffix
}

func init() {
	tool.Register(ExecuteCodeTool{})
}
