package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/tool"
)

type FileInsertTool struct{}

func (FileInsertTool) Name() string { return "file_insert" }

func (FileInsertTool) Description() string {
	return "Inserts text into a file in the sandbox container before the specified line number." +
			"Use file_read first to see the current content and line numbers before inserting."
}

func (FileInsertTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file.",
			},
			"line": map[string]any{
				"type":        "integer",
				"description": "Line number before which content is inserted (must be 1 or greater).",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content to insert.",
			},
		},
		"required":             []string{"path", "line", "content"},
		"additionalProperties": false,
	}
}

func (FileInsertTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args struct {
			Path    string `json:"path"`
			Line    int    `json:"line"`
			Content string `json:"content"`
		}

		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("file_insert: invalid arguments: %w", err)
		}
		if strings.TrimSpace(args.Path) == "" {
			return "", fmt.Errorf("file_insert: path must not be empty")
		}
		if args.Line < 1 {
			return "", fmt.Errorf("file_insert: line must be >= 1")
		}

		// Helper to format the tool response JSON
		respond := func(linesAdded int) (string, error) {
			b, _ := json.Marshal(map[string]any{
				"path":                 args.Path,
				"inserted_before_line": args.Line,
				"lines_added":          linesAdded,
			})
			return string(b), nil
		}

		// Early exit if content is empty
		if args.Content == "" {
			return respond(0)
		}

		// Load config once
		cfg := tool.GetToolConfig()
		containerName := config.ReadEntry(cfg, "sandbox.container_name", "ceres-sandbox")
		timeout, err := time.ParseDuration(config.ReadEntry(cfg, "sandbox.bash.timeout", "60s"))
		if err != nil {
			return "", fmt.Errorf("file_insert: invalid timeout: %w", err)
		}

		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Read target file
		readCmd := fmt.Sprintf("cat -- %s", shellQuote(args.Path))
		stdout, stderr, exitCode, err := runInContainer(execCtx, getDockerClient(), containerName, readCmd)
		if err != nil {
			return "", err
		}
		if exitCode != 0 {
			return "", fmt.Errorf("file_insert: %s", strings.TrimSpace(stderr))
		}

		hasTrailingNewline := strings.HasSuffix(stdout, "\n")
		cleanStdout := strings.TrimSuffix(strings.ReplaceAll(stdout, "\r\n", "\n"), "\n")

		var lines []string
		if cleanStdout != "" {
			lines = strings.Split(cleanStdout, "\n")
		}

		if args.Line > len(lines)+1 {
			return "", fmt.Errorf("file_insert: line %d out of range (file has %d lines)", args.Line, len(lines))
		}

		// Process content to insert
		insertContent := strings.TrimSuffix(strings.ReplaceAll(args.Content, "\r\n", "\n"), "\n")
		insertLines := strings.Split(insertContent, "\n")

		// Assemble new lines
		idx := args.Line - 1
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
		writeCmd := fmt.Sprintf("echo %s | base64 -d > %s", shellQuote(b64), shellQuote(args.Path))

		_, stderr, exitCode, err = runInContainer(execCtx, getDockerClient(), containerName, writeCmd)
		if err != nil {
			return "", err
		}
		if exitCode != 0 {
			return "", fmt.Errorf("file_insert write failed: %s", strings.TrimSpace(stderr))
		}

		return respond(len(insertLines))
	}
}

func init() {
	tool.Register(FileInsertTool{})
}
