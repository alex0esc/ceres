
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

type MemoryReadTool struct{}

func NewMemoryReadTool() MemoryReadTool {
	return MemoryReadTool{}
}

func (MemoryReadTool) Name() string {
	return "memory_read"
}

func (MemoryReadTool) Description() string {
	return "Lists directory contents or reads memory files inside the agent's persistent memory directory. " +
		"Use action='list' to inspect files and folders (optionally recursive or scoped to a subpath). " +
		"Use action='read' to read file contents with line numbers prefixed (format: \"<line_number>: <content>\"). " +
		"Optionally provide 'start_line' and/or 'end_line' (1-based, inclusive) to slice the content."
}

func (MemoryReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "read"},
				"description": "Which read operation to perform: 'list' or 'read'.",
			},
			"path": map[string]any{
				"type": []string{"string", "null"},
				"description": "Required for action='read': relative path to the memory file. " +
					"Ignored for action='list'.",
			},
			"start_line": map[string]any{
				"type": []string{"integer", "null"},
				"description": "Only used for action='read': optional 1-based line number to start reading from (inclusive). " +
					"Defaults to line 1.",
			},
			"end_line": map[string]any{
				"type": []string{"integer", "null"},
				"description": "Only used for action='read': optional 1-based line number to stop reading at (inclusive). " +
					"Defaults to the end of file.",
			},
			"subpath": map[string]any{
				"type": []string{"string", "null"},
				"description": "Only used for action='list': optional subpath relative to memory directory. " +
					"Defaults to the root memory directory.",
			},
			"recursive": map[string]any{
				"type": []string{"boolean", "null"},
				"description": "Only used for action='list': if true, walks subdirectories recursively. " +
					"Defaults to false.",
			},
		},
		"required":             []string{"action", "path", "start_line", "end_line", "subpath", "recursive"},
		"additionalProperties": false,
	}
}

func (m MemoryReadTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		agentName := strings.TrimSpace(handle.Name())
		if agentName == "" {
			return "", fmt.Errorf("memory_read: agent handle returned empty name")
		}

		agentDir := filepath.Clean(filepath.Join(memoryBaseDir, filepath.Base(agentName)))

		var args struct {
			Action    string `json:"action"`
			Path      string `json:"path"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
			Subpath   string `json:"subpath"`
			Recursive bool   `json:"recursive"`
		}
		if argumentsJSON != "" {
			if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
				return "", fmt.Errorf("memory_read: invalid arguments: %w", err)
			}
		}

		switch args.Action {
		case "list":
			return memoryList(agentDir, args.Subpath, args.Recursive)
		case "read":
			if strings.TrimSpace(args.Path) == "" {
				return "", fmt.Errorf("memory_read: 'path' is required when action='read'")
			}
			if args.StartLine < 0 || args.EndLine < 0 {
				return "", fmt.Errorf("memory_read: start_line and end_line must be positive line numbers, got start_line=%d end_line=%d", args.StartLine, args.EndLine)
			}
			if args.StartLine > 0 && args.EndLine > 0 && args.StartLine > args.EndLine {
				return "", fmt.Errorf("memory_read: start_line (%d) must not be greater than end_line (%d)", args.StartLine, args.EndLine)
			}
			return memoryRead(agentDir, args.Path, args.StartLine, args.EndLine)
		case "":
			return "", fmt.Errorf("memory_read: 'action' is required (must be 'list' or 'read')")
		default:
			return "", fmt.Errorf("memory_read: unknown action %q (must be 'list' or 'read')", args.Action)
		}
	}
}

func memoryList(agentDir string, subpath string, recursive bool) (string, error) {
	agentDirClean := filepath.Clean(agentDir)
	targetDir := filepath.Clean(filepath.Join(agentDirClean, subpath))

	if targetDir != agentDirClean && !strings.HasPrefix(targetDir, agentDirClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("memory_list: subpath %q is outside of agent memory directory", subpath)
	}

	info, err := os.Stat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(targetDir, 0o755); mkErr != nil {
				return "", fmt.Errorf("memory_list: could not create directory %q: %w", targetDir, mkErr)
			}
			info, err = os.Stat(targetDir)
			if err != nil {
				return "", fmt.Errorf("memory_list: directory %q not readable after creation: %w", targetDir, err)
			}
		} else {
			return "", fmt.Errorf("memory_list: directory %q not readable: %w", targetDir, err)
		}
	}
	if !info.IsDir() {
		return "", fmt.Errorf("memory_list: %q is not a directory", targetDir)
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
			rel, relErr := filepath.Rel(agentDirClean, path)
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
			return "", fmt.Errorf("memory_list: error while walking %q: %w", targetDir, err)
		}
	} else {
		dirEntries, err := os.ReadDir(targetDir)
		if err != nil {
			return "", fmt.Errorf("memory_list: error reading %q: %w", targetDir, err)
		}
		for _, d := range dirEntries {
			rel := filepath.Join(strings.TrimPrefix(targetDir, agentDirClean+string(os.PathSeparator)), d.Name())
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

	relBaseDir, _ := filepath.Rel(memoryBaseDir, targetDir)

	out := struct {
		BaseDir string  `json:"base_dir"`
		Count   int     `json:"count"`
		Entries []entry `json:"entries"`
	}{
		BaseDir: relBaseDir,
		Count:   len(entries),
		Entries: entries,
	}

	result, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("memory_list: failed to marshal result: %w", err)
	}
	return string(result), nil
}

func memoryRead(agentDir string, path string, startLine, endLine int) (string, error) {
	agentDirClean := filepath.Clean(agentDir)
	targetPath := filepath.Clean(filepath.Join(agentDirClean, path))

	if targetPath == agentDirClean || !strings.HasPrefix(targetPath, agentDirClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("memory_read: path %q is outside of agent memory directory", path)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("memory_read: file %q does not exist", path)
		}
		return "", fmt.Errorf("memory_read: file %q not readable: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("memory_read: %q is a directory, not a file", path)
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return "", fmt.Errorf("memory_read: error reading %q: %w", path, err)
	}

	numberedContent := numberLines(string(data))
	if startLine > 0 || endLine > 0 {
		var err error
		numberedContent, err = sliceNumberedLines(numberedContent, startLine, endLine)
		if err != nil {
			return "", fmt.Errorf("memory_read: %w", err)
		}
	}

	relPath, err := filepath.Rel(agentDirClean, targetPath)
	if err != nil {
		relPath = path
	}

	out := struct {
		Path    string `json:"path"`
		Size    int    `json:"size"`
		Content string `json:"content"`
	}{
		Path:    relPath,
		Size:    len(data),
		Content: numberedContent,
	}

	result, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("memory_read: failed to marshal result: %w", err)
	}
	return string(result), nil
}
