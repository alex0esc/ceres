package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/tool"
)

// FileStrReplaceTool replaces a unique occurrence of a string inside a file
// in the sandbox container. The old string must match the file's current
// content exactly and appear exactly once; this avoids ambiguous edits and
// the line-shifting issues that come with line-number-based replacements.
type FileStrReplaceTool struct{}

func (FileStrReplaceTool) Name() string {
	return "file_str_replace"
}

func (FileStrReplaceTool) Description() string {
	return "Replaces an exact, unique occurrence of a string inside a file in the sandbox container. " +
		"old_str must match the file's current content exactly (including whitespace/indentation) and must appear exactly once in the file. " +
		"Use file_read first to see the current content before replacing."
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
				"type":        "string",
				"description": "The exact string to replace. Must match the file's current content exactly and occur exactly once.",
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

func (FileStrReplaceTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args struct {
			Path   string `json:"path"`
			OldStr string `json:"old_str"`
			NewStr string `json:"new_str"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("file_str_replace: invalid arguments: %w", err)
		}
		if args.Path == "" {
			return "", fmt.Errorf("file_str_replace: path must not be empty")
		}
		if args.OldStr == "" {
			return "", fmt.Errorf("file_str_replace: old_str must not be empty")
		}

		containerName := config.ReadEntry(tool.GetToolConfig(), "sandbox.container_name", "ceres-sandbox")

		timeout, err := time.ParseDuration(config.ReadEntry(tool.GetToolConfig(), "sandbox.bash.timeout", "60s"))
		if err != nil {
			return "", fmt.Errorf("file_str_replace: error while parsing sandbox.bash.timeout in toolconfig.toml")
		}

		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cli := getDockerClient()

		// Read the current file content.
		readCmd := fmt.Sprintf("cat -- %s", shellQuote(args.Path))
		stdout, stderr, exitCode, err := runInContainer(execCtx, cli, containerName, readCmd)
		if err != nil {
			return "", fmt.Errorf("file_str_replace: failed to read file from sandbox: %w", err)
		}
		if exitCode != 0 {
			return "", fmt.Errorf("file_str_replace: cat exited with code %d: %s", exitCode, strings.TrimSpace(stderr))
		}

		// Ensure old_str occurs exactly once, so the replacement is unambiguous.
		count := strings.Count(stdout, args.OldStr)
		if count == 0 {
			return "", fmt.Errorf("file_str_replace: old_str not found in %s", args.Path)
		}
		if count > 1 {
			return "", fmt.Errorf("file_str_replace: old_str is not unique in %s (found %d occurrences), add more context to make it unique", args.Path, count)
		}

		newContent := strings.Replace(stdout, args.OldStr, args.NewStr, 1)

		// Write the new content back using a heredoc with a random-ish
		// delimiter to avoid clashing with content that itself contains
		// "EOF"-like markers.
		writeCmd := fmt.Sprintf("cat > %s <<'CERES_EOF_MARKER'\n%s\nCERES_EOF_MARKER", shellQuote(args.Path), newContent)

		_, stderr, exitCode, err = runInContainer(execCtx, cli, containerName, writeCmd)
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
			Path:    args.Path,
			Success: true,
		}

		result, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("file_str_replace: failed to marshal result: %w", err)
		}
		return string(result), nil
	}
}

func init() {
	tool.Register(FileStrReplaceTool{})
}
