package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
)

// FileWriteTool writes content to a file inside the sandbox container,
// creating it (and any parent directories) if necessary, or overwriting it
// if it already exists. After writing, it returns useful metadata about the
// resulting file so the agent can immediately reason about line numbers for
// follow-up edits without needing a separate file_read call.
type FileWriteTool struct{}

func (FileWriteTool) Name() string {
	return "file_write"
}

func (FileWriteTool) Description() string {
	return "Writes content to a file inside the sandbox container, creating parent directories and the file itself if " +
		"needed, or overwriting it if it already exists. Returns metadata about the resulting file (line count, byte " +
		"size, whether it ends with a trailing newline)." +
		"PREFER other file editing methods if possible to avoid rewriting the whole code, e.g. file_instert or file_str_replace!"
}

func (FileWriteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or working-directory-relative path of the file to write inside the sandbox container.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The full content to write to the file. This replaces the entire file content if it already exists.",
			},
		},
		"required":             []string{"path", "content"},
		"additionalProperties": false,
	}
}

func (FileWriteTool) Handler() ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("file_write: invalid arguments: %w", err)
		}
		if args.Path == "" {
			return "", fmt.Errorf("file_write: path must not be empty")
		}

		containerName := config.ReadEntry(GetToolConfig(), "sandbox.container_name", "ceres-sandbox")
		timeout, err := time.ParseDuration(config.ReadEntry(GetToolConfig(), "sandbox.bash.timeout", "60s"))
		if err != nil {
			return "", fmt.Errorf("file_write: error while parsing sandbox.bash.timeout in toolconfig.toml")
		}

		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Ensure the parent directory exists, then write the content via a
		// heredoc fed to "cat > file". Using a quoted heredoc delimiter
		// ("EOF_FILE_WRITE") prevents the shell from expanding $variables,
		// backticks, etc. inside the content. We pick a delimiter unlikely to
		// collide with file content; if it somehow appears verbatim in the
		// content this would break, but that's an acceptable, well-understood
		// limitation of the heredoc approach.
		const delim = "CERES_FILE_WRITE_EOF"
		quotedPath := shellQuote(args.Path)
		cmd := fmt.Sprintf(
			"mkdir -p -- \"$(dirname -- %s)\" && cat > %s <<'%s'\n%s\n%s",
			quotedPath, quotedPath, delim, args.Content, delim,
		)

		stdout, stderr, exitCode, err := runInContainer(execCtx, getDockerClient(), containerName, cmd)
		if err != nil {
			return "", fmt.Errorf("file_write: failed to write file in sandbox: %w", err)
		}
		if exitCode != 0 {
			return "", fmt.Errorf("file_write: write exited with code %d: %s", exitCode, strings.TrimSpace(stderr))
		}
		_ = stdout // no meaningful stdout expected from the write itself

		// Gather metadata about the resulting file in a single follow-up
		// command: byte size, line count (via wc), and whether the file ends
		// with a trailing newline (by checking the last byte).
		statCmd := fmt.Sprintf(
			"wc -c < %s && wc -l < %s && tail -c 1 %s | wc -l",
			quotedPath, quotedPath, quotedPath,
		)
		statCtx, statCancel := context.WithTimeout(ctx, timeout)
		defer statCancel()

		statOut, statErr, statExit, err := runInContainer(statCtx, getDockerClient(), containerName, statCmd)
		if err != nil {
			return "", fmt.Errorf("file_write: failed to stat written file: %w", err)
		}
		if statExit != 0 {
			return "", fmt.Errorf("file_write: stat exited with code %d: %s", statExit, strings.TrimSpace(statErr))
		}

		byteSize, lineCountWc, endsWithNewline, parseErr := parseStatOutput(statOut)
		if parseErr != nil {
			return "", fmt.Errorf("file_write: failed to parse file metadata: %w", parseErr)
		}

		// wc -l counts newline characters, not lines-of-content. If the file
		// doesn't end with a trailing newline, the last (partial) line isn't
		// counted, so we add 1 in that case to reflect the actual number of
		// lines a human/editor would see (matching numberLines' semantics in
		// file_read, which also does not emit a phantom trailing line but
		// does count a final unterminated line as a real line).
		lineCount := lineCountWc
		if byteSize > 0 && !endsWithNewline {
			lineCount++
		}

		out := struct {
			Path               string `json:"path"`
			BytesWritten        int    `json:"bytes_written"`
			LineCount          int    `json:"line_count"`
			EndsWithNewline    bool   `json:"ends_with_newline"`
		}{
			Path:            args.Path,
			BytesWritten:    byteSize,
			LineCount:       lineCount,
			EndsWithNewline: endsWithNewline,
		}

		result, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("file_write: failed to marshal result: %w", err)
		}
		return string(result), nil
	}
}

// parseStatOutput parses the three-line output produced by statCmd:
//
//	line 1: byte size (wc -c)
//	line 2: newline count (wc -l)
//	line 3: 1 if the last byte is a newline, 0 otherwise (tail -c 1 | wc -l)
func parseStatOutput(stdout string) (byteSize int, lineCount int, endsWithNewline bool, err error) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 {
		return 0, 0, false, fmt.Errorf("expected 3 lines of stat output, got %d: %q", len(lines), stdout)
	}

	if _, err = fmt.Sscanf(strings.TrimSpace(lines[0]), "%d", &byteSize); err != nil {
		return 0, 0, false, fmt.Errorf("failed to parse byte size from %q: %w", lines[0], err)
	}
	if _, err = fmt.Sscanf(strings.TrimSpace(lines[1]), "%d", &lineCount); err != nil {
		return 0, 0, false, fmt.Errorf("failed to parse line count from %q: %w", lines[1], err)
	}

	var newlineFlag int
	if _, err = fmt.Sscanf(strings.TrimSpace(lines[2]), "%d", &newlineFlag); err != nil {
		return 0, 0, false, fmt.Errorf("failed to parse trailing-newline flag from %q: %w", lines[2], err)
	}
	endsWithNewline = newlineFlag == 1

	return byteSize, lineCount, endsWithNewline, nil
}

func init() {
	Register(FileWriteTool{})
}
