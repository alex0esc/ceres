package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alex0esc/ceres/pkg/tool"
)

// SkillReadTool reads the contents of a single file within the "skills/"
// directory. The base path is fixed to "skills/"; the required "path"
// parameter specifies the file to read, relative to that base directory.
type SkillReadTool struct{}


// skillReadMaxBytes limits how much of a file is returned in one call, to
// avoid flooding the model context with very large files.
const skillReadMaxBytes = 200_000

func (SkillReadTool) Name() string {
	return "skill_read"
}

func (SkillReadTool) Description() string {
	return "Reads the contents of a file within the 'skills/' directory and returns it as text. To find the right path for a skill you can use skill_view!"
}

func (SkillReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file relative to 'skills/'.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func (SkillReadTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args struct {
			Path string `json:"path"`
		}
		if argumentsJSON == "" {
			return "", fmt.Errorf("skill_read: missing required argument 'path'")
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("skill_read: invalid arguments: %w", err)
		}
		if strings.TrimSpace(args.Path) == "" {
			return "", fmt.Errorf("skill_read: 'path' must not be empty")
		}

		// Determine target file and guard against directory traversal.
		targetPath := filepath.Clean(filepath.Join(skillBaseDir, args.Path))
		baseClean := filepath.Clean(skillBaseDir)
		if targetPath != baseClean && !strings.HasPrefix(targetPath, baseClean+string(os.PathSeparator)) {
			return "", fmt.Errorf("skill_read: path %q is outside of %q", args.Path, skillBaseDir)
		}

		info, err := os.Stat(targetPath)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("skill_read: file %q does not exist", targetPath)
			}
			return "", fmt.Errorf("skill_read: file %q not readable: %w", targetPath, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("skill_read: %q is a directory, not a file", targetPath)
		}

		data, err := os.ReadFile(targetPath)
		if err != nil {
			return "", fmt.Errorf("skill_read: error reading %q: %w", targetPath, err)
		}

		truncated := false
		content := data
		if len(content) > skillReadMaxBytes {
			content = content[:skillReadMaxBytes]
			truncated = true
		}

		out := struct {
			Path      string `json:"path"`
			Size      int    `json:"size"`
			Truncated bool   `json:"truncated"`
			Content   string `json:"content"`
		}{
			Path:      targetPath,
			Size:      len(data),
			Truncated: truncated,
			Content:   string(content),
		}

		result, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("skill_read: failed to marshal result: %w", err)
		}
		return string(result), nil
	}
}

func init() {
	tool.Register(SkillReadTool{})
}
