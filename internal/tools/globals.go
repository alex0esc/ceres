package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/bwmarrin/discordgo"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// all subagents needed for subagent tools
var subAgents map[string]handles.AgentHandle = nil

// sandboxOpTimeout is the fixed, ample timeout used by the lightweight sandbox
// operations (file read/edit, view_image, discord file reads) that don't need
// a user-tunable, potentially long-running timeout like bash.
const sandboxOpTimeout = 10 * time.Second

// used by all sandbox-related tools (bash, read_file, write_file).
var dockerClient *client.Client = nil

func SetSubagents(agents map[string]handles.AgentHandle) {
	subAgents = agents
}

func getSubagents() map[string]handles.AgentHandle {
	if subAgents == nil {
		log.Fatal("subagents have not been intialized!")
	}
	return subAgents
}

// InitDockerClient always attempts to connect to the Docker daemon. If the
// daemon is unreachable (e.g. Docker is not installed on this host) it logs a
// warning and leaves the client nil, so Ceres still starts; sandbox tools then
// report "not connected to docker daemon" per call.
func InitDockerClient() error {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}

	host := config.ReadEntry(tool.GetToolConfig(), "sandbox.docker_host", "")
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		slog.Warn("failed to create docker client; sandbox tools will be unavailable", "error", err)
		return nil
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(pingCtx); err != nil {
		slog.Warn("could not connect to docker daemon; sandbox tools will be unavailable", "error", err)
		cli.Close()
		return nil
	}

	dockerClient = cli
	return nil
}

func CloseDockerClient() {
	if dockerClient != nil {
		dockerClient.Close()
		dockerClient = nil
	}
}

// runInContainer runs cmd via "bash -c" inside the given container using
// Docker's exec API and returns separated stdout/stderr plus the exit code.
// Reading of the exec's combined output stream is bound to ctx: if ctx is
// cancelled before the stream ends (e.g. the in-container timeout did not
// terminate the process for some reason, or the daemon connection hangs),
// the read is abandoned, the exec is killed as a best-effort fallback, and
// ctx.Err() is returned.
func runInContainer(ctx context.Context, containerName, cmd string) (stdout string, stderr string, exitCode int, err error) {
	if containerName == "" {
		return "", "", 0, fmt.Errorf("no sandbox container configured for this agent")
	}
	if dockerClient == nil {
		return "", "", 0, fmt.Errorf("not connected to docker daemon")
	}

	execConfig := container.ExecOptions{
		Cmd:          []string{"bash", "-c", cmd},
		AttachStdout: true,
		AttachStderr: true,
	}
	execCreateResp, err := dockerClient.ContainerExecCreate(ctx, containerName, execConfig)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := dockerClient.ContainerExecAttach(ctx, execCreateResp.ID, container.ExecAttachOptions{})
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
		killExecProcess(context.Background(), dockerClient, execCreateResp.ID)
		return stdoutBuf.String(), stderrBuf.String(), -1, fmt.Errorf("command timed out: %w", ctx.Err())
	}

	inspectResp, err := dockerClient.ContainerExecInspect(ctx, execCreateResp.ID)
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

// discordMaxMessageLength is the maximum number of characters allowed per
// Discord message. Discord's hard limit is 2000 for normal messages (up to
// 4000 in some boosted contexts); we stay safely under that.
const discordMaxMessageLength = 1900

// sendChunked sends msg to the given channel, splitting it into multiple
// messages if it exceeds Discord's length limit.
func SendChunked(s *discordgo.Session, channelID, msg string) error {
	for _, chunk := range splitMessage(msg, discordMaxMessageLength) {
		if chunk == "" {
			continue
		}
		if _, err := s.ChannelMessageSend(channelID, chunk); err != nil {
			return fmt.Errorf("error while sending message to discord: %v", err)
		}
	}
	return nil
}

// splitMessage splits s into chunks of at most maxLen characters, preferring
// to break on newlines and, failing that, on spaces, so that words and lines
// aren't cut in the middle where possible.
func splitMessage(s string, maxLen int) []string {
	if len(s) <= maxLen {
		return []string{s}
	}

	var chunks []string
	for len(s) > maxLen {
		limit := maxLen

		// try to break on the last newline within the limit
		splitAt := -1
		if idx := lastIndexInRange(s, "\n", limit); idx > 0 {
			splitAt = idx + 1 // include the newline in the current chunk
		} else if idx := lastIndexInRange(s, " ", limit); idx > 0 {
			splitAt = idx + 1 // include the space in the current chunk
		} else {
			splitAt = limit // hard cut, no good break point found
		}

		chunks = append(chunks, s[:splitAt])
		s = s[splitAt:]
	}
	if len(s) > 0 {
		chunks = append(chunks, s)
	}
	return chunks
}

// lastIndexInRange returns the last index of sep within s[:limit], or -1 if
// not found.
func lastIndexInRange(s, sep string, limit int) int {
	if limit > len(s) {
		limit = len(s)
	}
	return strings.LastIndex(s[:limit], sep)
}

// readFileFromContainer stats, size-checks, reads, and base64-decodes a
// single file from inside the given sandbox container. Used by any tool
// that needs to pull file contents out of the sandbox (view_image, discord
// attachments, etc.).
func readFileFromContainer(ctx context.Context, containerName string, timeout time.Duration, maxSize int64, path string) ([]byte, int64, error) {
	if strings.TrimSpace(path) == "" {
		return nil, 0, fmt.Errorf("path must not be empty")
	}

	statCtx, statCancel := context.WithTimeout(ctx, timeout+2*killAfterBuffer)
	defer statCancel()

	statCmd := fmt.Sprintf("stat -c %%s -- %s", shellQuote(path))

	statOut, statErr, statExit, err := runInContainer(statCtx, containerName, statCmd)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to stat file in sandbox: %w", err)
	}
	if statExit != 0 {
		return nil, 0, fmt.Errorf("stat exited with code %d: %s", statExit, strings.TrimSpace(statErr))
	}

	size, err := strconv.ParseInt(strings.TrimSpace(statOut), 10, 64)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse file size %q: %w", strings.TrimSpace(statOut), err)
	}
	if size <= 0 {
		return nil, 0, fmt.Errorf("file %q is empty", path)
	}
	if size > maxSize {
		return nil, 0, fmt.Errorf("file is %d bytes, which exceeds the configured limit of %d bytes", size, maxSize)
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout+2*killAfterBuffer)
	defer cancel()

	cmd := fmt.Sprintf("base64 -w 0 -- %s", shellQuote(path))

	stdout, stderr, exitCode, err := runInContainer(execCtx, containerName, cmd)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read file from sandbox: %w", err)
	}
	if exitCode != 0 {
		return nil, 0, fmt.Errorf("base64 exited with code %d: %s", exitCode, strings.TrimSpace(stderr))
	}

	base64Data := strings.TrimSpace(stdout)
	if base64Data == "" {
		return nil, 0, fmt.Errorf("file %q produced empty base64 data", path)
	}

	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to decode file data: %w", err)
	}

	return data, size, nil
}
