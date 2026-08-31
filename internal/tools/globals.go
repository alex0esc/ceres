package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/bwmarrin/discordgo"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

//direcotry where all skills are stored
var memoryBaseDir string

//all subagents needed for subagent tools
var subAgents map[string]handles.AgentHandle = nil

// used by all sandbox-related tools (bash, read_file, write_file).
var dockerClient *client.Client = nil


func SetMemoryDir(path string) {
	memoryBaseDir = path
}


func SetSubagents(agents map[string]handles.AgentHandle) {
	subAgents = agents
}

func getSubagents() map[string]handles.AgentHandle {
	if subAgents == nil {
		log.Fatal("subagents have not been intialized!")
	}
	return subAgents
}


func InitDockerClient() error {
	active := config.ReadEntry(tool.GetToolConfig(), "sandbox.active", false)
	if !active {
		return nil
	}

	opts := []client.Opt{client.WithAPIVersionNegotiation()}

	host := config.ReadEntry(tool.GetToolConfig(), "sandbox.docker_host", "")
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return fmt.Errorf("failed to initialize docker client: %w", err)
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


func getDockerClient() *client.Client {
	if dockerClient == nil {
		log.Fatal("docker client not active or initialized!")
	}
	return dockerClient	
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
