package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
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
type BashTool struct{}

func (BashTool) Name() string {
	return "bash"
}

func (BashTool) Description() string {
	maxTimeout, err := time.ParseDuration(config.ReadEntry(tool.GetToolConfig(), "sandbox.timeout", "120s"))
	if err != nil {
		maxTimeout = 60 * time.Second
	}
	return fmt.Sprintf(
		"Executes a bash command inside an isolated sandbox docker container and returns stdout, stderr, and the exit code.\n"+
			"Use this for running shell commands, scripts, or for executing and debugging code you wrote.\n" +
			"The maximum allowed timeout is %s; you may optionally specify a shorter one via the \"timeout_seconds\" argument.",
		maxTimeout,
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

func (BashTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
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

		containerName := config.ReadEntry(tool.GetToolConfig(), "sandbox.container_name", "ceres-sandbox")

		maxTimeout, err := time.ParseDuration(config.ReadEntry(tool.GetToolConfig(), "sandbox.timeout", "120s"))
		if err != nil {
			return "", fmt.Errorf("bash: error while parsing sandbox.bash.timeout in toolconfig.toml")
		}
		if maxTimeout <= 0 {
			return "", fmt.Errorf("bash: sandbox.bash.timeout must be positive")
		}

		sandboxTimeout := maxTimeout
		if args.TimeoutSeconds != nil {
			requested := time.Duration(*args.TimeoutSeconds * float64(time.Second))
			if requested <= 0 {
				return "", fmt.Errorf("bash: timeout_seconds must be greater than 0")
			}
			if requested > maxTimeout {
				return "", fmt.Errorf("bash: timeout_seconds (%s) exceeds the maximum allowed timeout (%s)", requested, maxTimeout)
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

		stdout, stderr, exitCode, err := runInContainer(execCtx, getDockerClient(), containerName, wrappedCmd)
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
// Reading of the exec's combined output stream is bound to ctx: if ctx is
// cancelled before the stream ends (e.g. the in-container timeout did not
// terminate the process for some reason, or the daemon connection hangs),
// the read is abandoned, the exec is killed as a best-effort fallback, and
// ctx.Err() is returned.
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
	readDone := make(chan error, 1)
	go func() {
		_, copyErr := demuxDockerStream(attachResp.Reader, &stdoutBuf, &stderrBuf)
		readDone <- copyErr
	}()

	select {
	case copyErr := <-readDone:
		if copyErr != nil && copyErr != io.EOF {
			return "", "", 0, fmt.Errorf("failed to read exec output: %w", copyErr)
		}
	case <-ctx.Done():
		attachResp.Close()
		killExecProcess(context.Background(), cli, execCreateResp.ID)
		return stdoutBuf.String(), stderrBuf.String(), -1, fmt.Errorf("command timed out: %w", ctx.Err())
	}

	inspectResp, err := cli.ContainerExecInspect(ctx, execCreateResp.ID)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to inspect exec result: %w", err)
	}
	return stdoutBuf.String(), stderrBuf.String(), inspectResp.ExitCode, nil
}

// killExecProcess is a best-effort fallback that terminates the process
// backing execID by inspecting its PID and issuing a SIGKILL inside the
// container. Errors are intentionally ignored.
func killExecProcess(ctx context.Context, cli *client.Client, execID string) {
	inspectResp, err := cli.ContainerExecInspect(ctx, execID)
	if err != nil || inspectResp.Pid == 0 {
		return
	}
	killExec, err := cli.ContainerExecCreate(ctx, inspectResp.ContainerID, container.ExecOptions{
		Cmd: []string{"kill", "-9", fmt.Sprintf("%d", inspectResp.Pid)},
	})
	if err != nil {
		return
	}
	_ = cli.ContainerExecStart(ctx, killExec.ID, container.ExecStartOptions{})
}

// demuxDockerStream splits Docker's multiplexed exec stream (stdcopy format)
// into separate stdout/stderr writers.
func demuxDockerStream(reader io.Reader, stdout, stderr io.Writer) (int64, error) {
	return stdcopy.StdCopy(stdout, stderr, reader)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func init() {
	tool.Register(BashTool{})
}
