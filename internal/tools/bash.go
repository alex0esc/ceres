
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// killAfterBuffer is added on top of the in-container "timeout" duration to
// give the coreutils timeout(1) process a moment to actually terminate the
// command (SIGTERM, then SIGKILL after --kill-after) before Go's exec
// context also expires.
const killAfterBuffer = 5 * time.Second

// BashTool executes a shell command inside a separate sandbox container via
// the Docker SDK. It is intended to give the agent an isolated environment
// to run arbitrary commands without affecting the host or the agent's own
// container.
type BashTool struct {
	maxTimeout    time.Duration
	containerName string
}

// NewBashTool constructs a BashTool, reading all relevant config values once
// up front so the handler doesn't need to re-read config on every call.
func NewBashTool() BashTool {
	maxTimeout, err := time.ParseDuration(config.ReadEntry(tool.GetToolConfig(), "sandbox.timeout", "120s"))
	if err != nil || maxTimeout <= 0 {
		log.Fatal("Could not read sandbox.timeout or invalid value in tool config!")
	}

	containerName := config.ReadEntry(tool.GetToolConfig(), "sandbox.container_name", "ceres-sandbox")

	return BashTool{
		maxTimeout:    maxTimeout,
		containerName: containerName,
	}
}


func (BashTool) Name() string {
	return "bash"
}

func (b BashTool) Description() string {
	return fmt.Sprintf(
		"Executes a bash command inside an isolated sandbox docker container and returns stdout, stderr, and the exit code.\n"+
			"Use this for running shell commands, scripts, or for executing and debugging code you wrote.\n"+
			"The maximum allowed timeout is %s; you may optionally specify a shorter one via the \"timeout_seconds\" argument.",
		b.maxTimeout,
	)
}

func (BashTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The bash command to execute inside the sandbox container.",
			},
			"timeout_seconds": map[string]any{
				"type":        []string{"number", "null"},
				"description": "Optional. Maximum time in seconds the command may run before being killed.",
			},
		},
		"required":             []string{"command", "timeout_seconds"},
		"additionalProperties": false,
	}
}

func (b BashTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args struct {
			Command        string   `json:"command"`
			TimeoutSeconds *float64 `json:"timeout_seconds"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("bash: invalid arguments: %w", err)
		}
		if args.Command == "" {
			return "", fmt.Errorf("bash: command must not be empty")
		}

		sandboxTimeout := b.maxTimeout
		if args.TimeoutSeconds != nil {
			requested := time.Duration(*args.TimeoutSeconds * float64(time.Second))
			if requested <= 0 {
				return "", fmt.Errorf("bash: timeout_seconds must be greater than 0")
			}
			if requested > b.maxTimeout {
				return "", fmt.Errorf("bash: timeout_seconds (%s) exceeds the maximum allowed timeout (%s)", requested, b.maxTimeout)
			}
			sandboxTimeout = requested
		}

		// The Go-side context is the hard upper bound (safety net). It must
		// be strictly larger than sandboxTimeout so that "timeout" inside
		// the container gets a real chance to fire (and, via --kill-after,
		// escalate to SIGKILL) before we give up on reading its output.
		execCtx, cancel := context.WithTimeout(ctx, sandboxTimeout+2*killAfterBuffer)
		defer cancel()

		wrappedCmd := fmt.Sprintf(
			"timeout --kill-after=%.0fs %.3fs bash -c %s",
			killAfterBuffer.Seconds(),
			sandboxTimeout.Seconds(),
			shellQuote(args.Command),
		)

		stdout, stderr, exitCode, err := runInContainer(execCtx, getDockerClient(), b.containerName, wrappedCmd)
		if err != nil {
			return "", fmt.Errorf("bash: failed to execute command in sandbox: %w", err)
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
			return "", fmt.Errorf("bash: failed to marshal result: %w", err)
		}
		return string(result), nil
	}
}
