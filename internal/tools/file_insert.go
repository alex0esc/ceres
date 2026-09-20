
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

// FileInsertTool inserts text into a file inside the sandbox container,
// before a given line number.
type FileInsertTool struct {
	containerName string
	timeout       time.Duration
}

// NewFileInsertTool constructs a FileInsertTool, reading all relevant config
// values once up front.
func NewFileInsertTool() FileInsertTool {
	cfg := tool.GetToolConfig()
	containerName := config.ReadEntry(cfg, "sandbox.container_name", "ceres-sandbox")
	timeout := config.ReadEntry(cfg, "sandbox.timeout", time.Second*120)

	return FileInsertTool{
		containerName: containerName,
		timeout:       timeout,
	}
}

func (FileInsertTool) Name() string {
	return "file_insert"
}

func (FileInsertTool) Description() string {
	return "Inserts 'content' into an existing file inside the sandbox container, before the given 'line' number " +
		"(1-based). To append at the end of the file, use the line number after the last line. " +
		"Use file_read first to see the current content and line numbers. " +
		"Returns the line the content was inserted before and the number of lines added."
}

func (FileInsertTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or working-directory-relative path of the file to edit inside the sandbox container.",
			},
			"line": map[string]any{
				"type":        "integer",
				"description": "Line number before which the content is inserted (must be 1 or greater).",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to insert.",
			},
		},
		"required":             []string{"path", "line", "content"},
		"additionalProperties": false,
	}
}

func (t FileInsertTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args struct {
			Path    string `json:"path"`
			Line    int    `json:"line"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("file_insert: invalid arguments: %w", err)
		}
		return t.fileInsert(ctx, args.Path, args.Line, args.Content)
	}
}

// fileInsert inserts content into a file in the sandbox container before
// the specified line number.
func (t FileInsertTool) fileInsert(ctx context.Context, path string, line int, content string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("file_insert: path must not be empty")
	}
	if line < 1 {
		return "", fmt.Errorf("file_insert: line must be >= 1")
	}

	// Helper to format the tool response JSON
	respond := func(linesAdded int) (string, error) {
		b, _ := json.Marshal(map[string]any{
			"path":                 path,
			"inserted_before_line": line,
			"lines_added":          linesAdded,
		})
		return string(b), nil
	}

	// Early exit if content is empty
	if content == "" {
		return respond(0)
	}

	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Read target file
	readCmd := fmt.Sprintf("cat -- %s", shellQuote(path))
	stdout, stderr, exitCode, err := runInContainer(execCtx, t.containerName, readCmd)
	if err != nil {
		return "", fmt.Errorf("file_insert: failed to read file from sandbox: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("file_insert: cat exited with code %d: %s", exitCode, strings.TrimSpace(stderr))
	}

	hasTrailingNewline := strings.HasSuffix(stdout, "\n")
	cleanStdout := strings.TrimSuffix(strings.ReplaceAll(stdout, "\r\n", "\n"), "\n")
	var lines []string
	if cleanStdout != "" {
		lines = strings.Split(cleanStdout, "\n")
	}
	if line > len(lines)+1 {
		return "", fmt.Errorf("file_insert: line %d out of range (file has %d lines)", line, len(lines))
	}

	// Process content to insert
	insertContent := strings.TrimSuffix(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	insertLines := strings.Split(insertContent, "\n")

	// Assemble new lines
	idx := line - 1
	newLines := make([]string, 0, len(lines)+len(insertLines))
	newLines = append(newLines, lines[:idx]...)
	newLines = append(newLines, insertLines...)
	newLines = append(newLines, lines[idx:]...)
	newContent := strings.Join(newLines, "\n")
	if hasTrailingNewline || len(lines) == 0 {
		newContent += "\n"
	}

	// Write safely via base64 to avoid shell escaping & heredoc issues
	b64 := base64.StdEncoding.EncodeToString([]byte(newContent))
	writeCmd := fmt.Sprintf("echo %s | base64 -d > %s", shellQuote(b64), shellQuote(path))
	_, stderr, exitCode, err = runInContainer(execCtx, t.containerName, writeCmd)
	if err != nil {
		return "", fmt.Errorf("file_insert: failed to write file to sandbox: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("file_insert: write exited with code %d: %s", exitCode, strings.TrimSpace(stderr))
	}

	return respond(len(insertLines))
}
