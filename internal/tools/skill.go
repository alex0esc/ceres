package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// SkillTool bundles read and list operations on files within the
// "skills/" directory into a single tool, dispatched via the "action"
// parameter ("list" or "read"). The base path is always fixed to
// "skills/"; both operations only accept paths relative to it and guard
// against directory traversal outside of it.
type SkillTool struct{}

// skillReadMaxBytes limits how much of a file is returned in one call, to
// avoid flooding the model context with very large files.
const skillReadMaxBytes = 200_000

func (SkillTool) Name() string {
	return "skill"
}

func (SkillTool) Description() string {
	return "Lists or reads files within the 'skills/' directory. " +
		"Use action='list' to list file and folder names (optionally recursive, optionally scoped to a subpath), " +
		"or action='read' with 'path' to read the contents of a single skill file. " +
		"Use action='list' first to find the right path for action='read'."
}

func (SkillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "read"},
				"description": "Which operation to perform: 'list' to list directory contents, 'read' to read a single file's contents.",
			},
			"path": map[string]any{
				"type": []string{"string", "null"},
				"description": "Required for action='read': path to the file relative to 'skills/'. " +
					"Ignored for action='list'.",
			},
			"subpath": map[string]any{
				"type": []string{"string", "null"},
				"description": "Only used for action='list': optional subpath relative to 'skills/' (e.g. 'docx') " +
					"to list only its contents. Defaults to the root directory 'skills/'.",
			},
			"recursive": map[string]any{
				"type":        []string{"boolean", "null"},
				"description": "Only used for action='list': if true, all subdirectories are also searched recursively. Defaults to false (top level only).",
			},
		},
		"required":             []string{"action", "path", "subpath", "recursive"},
		"additionalProperties": false,
	}
}

func (SkillTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args struct {
			Action    string `json:"action"`
			Path      string `json:"path"`
			Subpath   string `json:"subpath"`
			Recursive bool   `json:"recursive"`
		}
		if argumentsJSON != "" {
			if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
				return "", fmt.Errorf("skill: invalid arguments: %w", err)
			}
		}

		switch args.Action {
		case "list":
			return skillList(args.Subpath, args.Recursive)
		case "read":
			if strings.TrimSpace(args.Path) == "" {
				return "", fmt.Errorf("skill: 'path' is required when action='read'")
			}
			return skillRead(args.Path)
		case "":
			return "", fmt.Errorf("skill: 'action' is required (must be 'list' or 'read')")
		default:
			return "", fmt.Errorf("skill: unknown action %q (must be 'list' or 'read')", args.Action)
		}
	}
}

// skillRead reads a single file within skillBaseDir, relative to it.
func skillRead(path string) (string, error) {
	targetPath := filepath.Clean(filepath.Join(skillBaseDir, path))
	baseClean := filepath.Clean(skillBaseDir)
	if targetPath != baseClean && !strings.HasPrefix(targetPath, baseClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("skill_read: path %q is outside of %q", path, skillBaseDir)
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

// skillList lists directory contents within skillBaseDir, optionally
// scoped to a subpath and optionally recursive. If the target directory
// does not yet exist, it is created (matching prior SkillListTool behavior).
func skillList(subpath string, recursive bool) (string, error) {
	targetDir := filepath.Clean(filepath.Join(skillBaseDir, subpath))
	baseClean := filepath.Clean(skillBaseDir)
	if targetDir != baseClean && !strings.HasPrefix(targetDir, baseClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("skill_list: subpath %q is outside of %q", subpath, skillBaseDir)
	}

	info, err := os.Stat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory does not exist yet -> create it automatically.
			if mkErr := os.MkdirAll(targetDir, 0o755); mkErr != nil {
				return "", fmt.Errorf("skill_list: could not create directory %q: %w", targetDir, mkErr)
			}
			info, err = os.Stat(targetDir)
			if err != nil {
				return "", fmt.Errorf("skill_list: directory %q not readable after creation: %w", targetDir, err)
			}
		} else {
			return "", fmt.Errorf("skill_list: directory %q not readable: %w", targetDir, err)
		}
	}
	if !info.IsDir() {
		return "", fmt.Errorf("skill_list: %q is not a directory", targetDir)
	}

	type entry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
	}
	var entries []entry

	if recursive {
		err = filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == targetDir {
				return nil
			}
			rel, relErr := filepath.Rel(skillBaseDir, path)
			if relErr != nil {
				rel = path
			}
			entries = append(entries, entry{
				Name:  d.Name(),
				Path:  rel,
				IsDir: d.IsDir(),
			})
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("skill_list: error while walking %q: %w", targetDir, err)
		}
	} else {
		dirEntries, err := os.ReadDir(targetDir)
		if err != nil {
			return "", fmt.Errorf("skill_list: error reading %q: %w", targetDir, err)
		}
		for _, d := range dirEntries {
			rel := filepath.Join(strings.TrimPrefix(targetDir, filepath.Clean(skillBaseDir)+string(os.PathSeparator)), d.Name())
			rel = strings.TrimPrefix(rel, string(os.PathSeparator))
			if strings.TrimSpace(subpath) == "" {
				rel = d.Name()
			}
			entries = append(entries, entry{
				Name:  d.Name(),
				Path:  rel,
				IsDir: d.IsDir(),
			})
		}
	}

	out := struct {
		BaseDir string  `json:"base_dir"`
		Count   int     `json:"count"`
		Entries []entry `json:"entries"`
	}{
		BaseDir: targetDir,
		Count:   len(entries),
		Entries: entries,
	}

	result, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("skill_list: failed to marshal result: %w", err)
	}
	return string(result), nil
}

func init() {
	tool.Register(SkillTool{})
}
