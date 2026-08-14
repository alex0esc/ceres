package tools


import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/tool"
)

// FileReadTool reads a file inside the sandbox container and returns its
// content with each line prefixed by its line number. The line numbers are
// meant to be used together with a future "replace_lines" style tool, so the
// agent can target specific lines without having to rewrite the whole file.
type FileReadTool struct{}

func (FileReadTool) Name() string {
	return "file_read"
}

func (FileReadTool) Description() string {
	maxSize, err := parseMaxFileSize()
	if err != nil {
		maxSize = 1 << 20 // 1 MiB display fallback
	}
	return fmt.Sprintf(
		"Reads a file inside the sandbox container and returns its content with each line prefixed by its line number "+
			"(format: \"<line_number>: <content>\"). Use the line numbers to target specific lines with file-editing tools. "+
			"Files larger than %d bytes are rejected; read a subset (e.g. via bash with head/sed) if a file exceeds this limit.",
		maxSize,
	)
}

func (FileReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or working-directory-relative path of the file to read inside the sandbox container.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func (FileReadTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("file_read: invalid arguments: %w", err)
		}
		if args.Path == "" {
			return "", fmt.Errorf("file_read: path must not be empty")
		}

		containerName := config.ReadEntry(tool.GetToolConfig(), "sandbox.container_name", "ceres-sandbox")

		timeout, err := time.ParseDuration(config.ReadEntry(tool.GetToolConfig(), "sandbox.bash.timeout", "60s"))
		if err != nil {
			return "", fmt.Errorf("file_read: error while parsing sandbox.bash.timeout in toolconfig.toml")
		}
		if timeout <= 0 {
			return "", fmt.Errorf("file_read: sandbox.bash.timeout must be positive")
		}

		maxSize, err := parseMaxFileSize()
		if err != nil {
			return "", fmt.Errorf("file_read: error while parsing file_read.max_size in toolconfig.toml: %w", err)
		}

		cli := getDockerClient()

		// Check the file size before reading it fully, so an oversized or
		// otherwise unusual file (huge log, device file, etc.) never gets
		// pulled entirely into memory just to be rejected afterwards.
		statCtx, statCancel := context.WithTimeout(ctx, timeout+2*killAfterBuffer)
		statCmd := fmt.Sprintf("stat -c %%s -- %s", shellQuote(args.Path))
		statOut, statErr, statExit, err := runInContainer(statCtx, cli, containerName, statCmd)
		statCancel()
		if err != nil {
			return "", fmt.Errorf("file_read: failed to stat file in sandbox: %w", err)
		}
		if statExit != 0 {
			return "", fmt.Errorf("file_read: stat exited with code %d: %s", statExit, strings.TrimSpace(statErr))
		}
		size, err := strconv.ParseInt(strings.TrimSpace(statOut), 10, 64)
		if err != nil {
			return "", fmt.Errorf("file_read: failed to parse file size %q: %w", strings.TrimSpace(statOut), err)
		}
		if size > maxSize {
			return "", fmt.Errorf("file_read: file is %d bytes, which exceeds the configured limit of %d bytes", size, maxSize)
		}

		execCtx, cancel := context.WithTimeout(ctx, timeout+2*killAfterBuffer)
		defer cancel()
		// "cat" is used instead of the docker SDK's file-copy API so we can
		// reuse the existing exec plumbing (runInContainer) and get a clear
		// stderr/exit code if the file is missing or unreadable.
		cmd := fmt.Sprintf("cat -- %s", shellQuote(args.Path))
		stdout, stderr, exitCode, err := runInContainer(execCtx, cli, containerName, cmd)
		if err != nil {
			return "", fmt.Errorf("file_read: failed to read file from sandbox: %w", err)
		}
		if exitCode != 0 {
			return "", fmt.Errorf("file_read: cat exited with code %d: %s", exitCode, strings.TrimSpace(stderr))
		}
		out := struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}{
			Path:    args.Path,
			Content: numberLines(stdout),
		}
		result, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("file_read: failed to marshal result: %w", err)
		}
		return string(result), nil
	}
}

// parseMaxFileSize reads and parses file_read.max_size from the tool config.
// The value is a plain byte count (e.g. "1048576"). Defaults to 1 MiB.
func parseMaxFileSize() (int64, error) {
	raw := config.ReadEntry(tool.GetToolConfig(), "file_read.max_size", "1048576")
	maxSize, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q: %w", raw, err)
	}
	if maxSize <= 0 {
		return 0, fmt.Errorf("must be a positive number of bytes, got %q", raw)
	}
	return maxSize, nil
}

// numberLines prefixes every line of content with its 1-based line number,
// e.g. "1: package tools". A trailing newline in content does not produce a
// phantom extra numbered line.
func numberLines(content string) string {
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%d: %s", i+1, line)
	}
	return b.String()
}

func init() {
	tool.Register(FileReadTool{})
}
