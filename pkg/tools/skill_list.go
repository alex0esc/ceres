package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillListTool lists all file names within the "skills/" directory
// (optionally recursively into a subfolder). The base path is fixed to
// "skills/"; the optional "subpath" parameter can be used to specify a
// subdirectory within it.
type SkillListTool struct{}

const skillListBaseDir = "skills/"

func (SkillListTool) Name() string {
	return "skill_list"
}

func (SkillListTool) Description() string {
	return "Lists all file and folder names in the 'skills/' directory. " +
		"A subpath within 'skills/' can be specified to list only its contents. " +
		"Listing can be recursive (including subdirectories)."
}

func (SkillListTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subpath": map[string]any{
				"type":        "string",
				"description": "Optional subpath relative to 'skills/' (e.g. 'docx'). Defaults to the root directory 'skills/'.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "If true, all subdirectories are also searched recursively. Defaults to false (top level only).",
			},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func (SkillListTool) Handler() ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args struct {
			Subpath   string `json:"subpath"`
			Recursive bool   `json:"recursive"`
		}
		// argumentsJSON can be empty ("" or "{}"), since all fields are optional.
		if argumentsJSON != "" {
			if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
				return "", fmt.Errorf("skill_list: invalid arguments: %w", err)
			}
		}

		// Determine target directory and guard against directory traversal.
		targetDir := filepath.Clean(filepath.Join(skillListBaseDir, args.Subpath))
		baseClean := filepath.Clean(skillListBaseDir)
		if targetDir != baseClean && !strings.HasPrefix(targetDir, baseClean+string(os.PathSeparator)) {
			return "", fmt.Errorf("skill_list: subpath %q is outside of %q", args.Subpath, skillListBaseDir)
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

		if args.Recursive {
			err = filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if path == targetDir {
					return nil
				}
				rel, relErr := filepath.Rel(skillListBaseDir, path)
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
				rel := filepath.Join(strings.TrimPrefix(targetDir, filepath.Clean(skillListBaseDir)+string(os.PathSeparator)), d.Name())
				rel = strings.TrimPrefix(rel, string(os.PathSeparator))
				if strings.TrimSpace(args.Subpath) == "" {
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
}

func init() {
	Register(SkillListTool{})
}
