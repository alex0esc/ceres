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

// FileStrReplaceTool replaces a unique string occurrence inside a file in
// the sandbox container.
type FileStrReplaceTool struct {
	containerName string
	timeout       time.Duration
}

// NewFileStrReplaceTool constructs a FileStrReplaceTool, reading all
// relevant config values once up front.
func NewFileStrReplaceTool() FileStrReplaceTool {
	cfg := tool.GetToolConfig()
	containerName := config.ReadEntry(cfg, "sandbox.container_name", "ceres-sandbox")
	timeout := config.ReadEntry(cfg, "sandbox.timeout", time.Second*120)

	return FileStrReplaceTool{
		containerName: containerName,
		timeout:       timeout,
	}
}

func (FileStrReplaceTool) Name() string {
	return "file_str_replace"
}

func (FileStrReplaceTool) Description() string {
	return "Replaces a unique, exact occurrence of 'old_str' with 'new_str' in a file inside the sandbox container. " +
		"'old_str' must match the file's current content exactly (including whitespace/indentation) and occur " +
		"exactly once; if it is not unique, include more surrounding context. 'new_str' may be empty to delete " +
		"'old_str'. Use file_read first to see the current content."
}

func (FileStrReplaceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or working-directory-relative path of the file to edit inside the sandbox container.",
			},
			"old_str": map[string]any{
				"type": "string",
				"description": "The exact string to replace. Must match the file's current content exactly and " +
					"occur exactly once.",
			},
			"new_str": map[string]any{
				"type":        "string",
				"description": "The string to replace old_str with. Can be empty to delete old_str.",
			},
		},
		"required":             []string{"path", "old_str", "new_str"},
		"additionalProperties": false,
	}
}

func (t FileStrReplaceTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args struct {
			Path   string `json:"path"`
			OldStr string `json:"old_str"`
			NewStr string `json:"new_str"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("file_str_replace: invalid arguments: %w", err)
		}
		return t.fileStrReplace(ctx, args.Path, args.OldStr, args.NewStr)
	}
}

// fileStrReplace replaces a unique occurrence of a string inside a file in
// the sandbox container. The old string must match the file's current
// content exactly and appear exactly once; this avoids ambiguous edits and
// the line-shifting issues that come with line-number-based replacements.
// All \r\n line endings are normalized to \n, in the file as well as in
// old_str and new_str; the file is therefore written back with LF endings.
func (t FileStrReplaceTool) fileStrReplace(ctx context.Context, path, oldStr, newStr string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file_str_replace: path must not be empty")
	}
	if oldStr == "" {
		return "", fmt.Errorf("file_str_replace: old_str must not be empty")
	}

	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Read the current file content.
	readCmd := fmt.Sprintf("cat -- %s", shellQuote(path))
	stdout, stderr, exitCode, err := runInContainer(execCtx, t.containerName, readCmd)
	if err != nil {
		return "", fmt.Errorf("file_str_replace: failed to read file from sandbox: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("file_str_replace: cat exited with code %d: %s", exitCode, strings.TrimSpace(stderr))
	}

	// Normalize all line endings to \n (file content and both strings), so
	// that matching and the resulting file are independent of \r\n vs \n.
	stdout = strings.ReplaceAll(stdout, "\r\n", "\n")
	oldStr = strings.ReplaceAll(oldStr, "\r\n", "\n")
	newStr = strings.ReplaceAll(newStr, "\r\n", "\n")

	// Ensure old_str occurs exactly once, so the replacement is unambiguous.
	count := strings.Count(stdout, oldStr)
	if count == 0 {
		return "", fmt.Errorf("file_str_replace: old_str not found in %s", path)
	}
	if count > 1 {
		return "", fmt.Errorf("file_str_replace: old_str is not unique in %s (found %d occurrences), add more context to make it unique", path, count)
	}
	newContent := strings.Replace(stdout, oldStr, newStr, 1)

	// Write the new content back via base64 piped into the file. This
	// avoids the heredoc approach's trailing-newline ambiguity (a heredoc
	// unconditionally inserts a newline between the content and the
	// closing delimiter line, which either duplicates an existing
	// trailing newline or fabricates one where none existed) as well as
	// any clashes with "EOF"-like markers inside the content.
	b64 := base64.StdEncoding.EncodeToString([]byte(newContent))
	writeCmd := fmt.Sprintf("echo %s | base64 -d > %s", shellQuote(b64), shellQuote(path))
	_, stderr, exitCode, err = runInContainer(execCtx, t.containerName, writeCmd)
	if err != nil {
		return "", fmt.Errorf("file_str_replace: failed to write file to sandbox: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("file_str_replace: write exited with code %d: %s", exitCode, strings.TrimSpace(stderr))
	}

	out := struct {
		Path    string `json:"path"`
		Success bool   `json:"success"`
	}{
		Path:    path,
		Success: true,
	}
	result, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("file_str_replace: failed to marshal result: %w", err)
	}
	return string(result), nil
}
