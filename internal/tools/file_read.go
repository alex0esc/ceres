
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// FileReadTool reads a file inside the sandbox container and returns its content with line numbers
type FileReadTool struct{}

func (FileReadTool) Name() string {
	return "file_read"
}

func (FileReadTool) Description() string {
	maxSize, err := parseMaxFileSize()
	if err != nil {
		log.Fatalf("error while parsing file_read.max_size: %v", err)
	}
	return fmt.Sprintf(
		"Reads a file inside the sandbox container and returns its content with each line prefixed by its line number "+
			"(format: \"<line_number>: <content>\"). Use the line numbers to target specific lines with file-editing tools. "+
			"Optionally provide 'start_line' and/or 'end_line' (1-based, inclusive) to only return a slice of the file "+
			"instead of the full content; Files larger than %d bytes are rejected.",
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
			"start_line": map[string]any{
				"type":        []string{"integer", "null"},
				"description": "Optional 1-based line number to start reading from (inclusive). Defaults to the first line.",
			},
			"end_line": map[string]any{
				"type":        []string{"integer", "null"},
				"description": "Optional 1-based line number to stop reading at (inclusive). Defaults to the last line.",
			},
		},
		"required":             []string{"path", "start_line", "end_line"},
		"additionalProperties": false,
	}
}

func (FileReadTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args struct {
			Path      string `json:"path"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("file_read: invalid arguments: %w", err)
		}
		if args.Path == "" {
			return "", fmt.Errorf("file_read: path must not be empty")
		}
		if args.StartLine < 0 || args.EndLine < 0 {
			return "", fmt.Errorf("file_read: start_line and end_line must be positive line numbers, got start_line=%d end_line=%d", args.StartLine, args.EndLine)
		}
		if args.StartLine > 0 && args.EndLine > 0 && args.StartLine > args.EndLine {
			return "", fmt.Errorf("file_read: start_line (%d) must not be greater than end_line (%d)", args.StartLine, args.EndLine)
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
			log.Fatalf("error while parsing file_read.max_size: %v", err)
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

		content := numberLines(stdout)
		if args.StartLine > 0 || args.EndLine > 0 {
			content, err = sliceNumberedLines(content, args.StartLine, args.EndLine)
			if err != nil {
				return "", fmt.Errorf("file_read: %w", err)
			}
		}

		out := struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}{
			Path:    args.Path,
			Content: content,
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

func sliceNumberedLines(numbered string, start, end int) (string, error) {
	if numbered == "" {
		return "", fmt.Errorf("file is empty, no lines to select")
	}
	lines := strings.Split(numbered, "\n")
	total := len(lines)

	if start <= 0 {
		start = 1
	}
	if end <= 0 || end > total {
		end = total
	}
	if start > end {
		return "", fmt.Errorf("start_line %d is beyond the file's available lines (1-%d)", start, total)
	}

	return strings.Join(lines[start-1:end], "\n"), nil
}

func init() {
	tool.Register(FileReadTool{})
}
