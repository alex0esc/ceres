package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// BashTool executes a shell command inside a separate sandbox container via
// the Docker SDK. It is intended to give the agent an isolated environment
// to run arbitrary commands without affecting the host or the agent's own
// container.
type BashTool struct{}

func (BashTool) Name() string {
	return "bash"
}

func (BashTool) Description() string {
	return "Executes a bash command inside an isolated sandbox docker container and returns stdout, stderr, and the exit code. " +
		"Use this for running shell commands, scripts, or for executing and debugging code you wrote. " +
		"For programms that do not terminate use the Linux-Tool timeout for example 'timeout 5s python3 game.py'."
}

func (BashTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The bash command to execute inside the sandbox container.",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func (BashTool) Handler() ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("bash: invalid arguments: %w", err)
		}
		if args.Command == "" {
			return "", fmt.Errorf("bash: command must not be empty")
		}

		containerName := config.ReadEntry(GetToolConfig(), "sandbox.container_name", "ceres-sandbox")
		

		timeout, err := time.ParseDuration(config.ReadEntry(GetToolConfig(), "sandbox.bash.timeout", "60s"))
		if err != nil {
			return "", fmt.Errorf("Error while parsing sandbox.bash.timeout in toolconfig.toml")
		}

		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		stdout, stderr, exitCode, err := runInContainer(execCtx, getDockerClient(), containerName, args.Command)
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



// runInContainer runs cmd via "bash -c" inside the given container using
// Docker's exec API and returns separated stdout/stderr plus the exit code.
func runInContainer(ctx context.Context, cli *client.Client, containerName, cmd string) (stdout string, stderr string, exitCode int, err error) {
	execConfig := container.ExecOptions{
		Cmd:          []string{"bash", "-c", cmd},
		AttachStdout: true,
		AttachStderr: true,
	}

	execCreateResp, err := cli.ContainerExecCreate(ctx, containerName, execConfig)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := cli.ContainerExecAttach(ctx, execCreateResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	if _, err := demuxDockerStream(attachResp.Reader, &stdoutBuf, &stderrBuf); err != nil && err != io.EOF {
		return "", "", 0, fmt.Errorf("failed to read exec output: %w", err)
	}

	inspectResp, err := cli.ContainerExecInspect(ctx, execCreateResp.ID)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to inspect exec result: %w", err)
	}

	return stdoutBuf.String(), stderrBuf.String(), inspectResp.ExitCode, nil
}

// demuxDockerStream splits Docker's multiplexed exec stream (stdcopy format)
// into separate stdout/stderr writers. Docker's own stdcopy package could be
// used here instead (github.com/docker/docker/pkg/stdcopy); this is a
// placeholder call to make that dependency explicit.
func demuxDockerStream(reader io.Reader, stdout, stderr io.Writer) (int64, error) {
	return stdcopy.StdCopy(stdout, stderr, reader)
}

func init() {
	Register(BashTool{})
}
