
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

type MemoryEditTool struct {
	maxBytes int
}

func NewMemoryEditTool() MemoryEditTool {
	maxBytes := config.ReadEntry(tool.GetToolConfig(), "memory.write_max_bytes", 200_000)
	return MemoryEditTool{
		maxBytes: maxBytes,
	}
}

func (MemoryEditTool) Name() string {
	return "memory_edit"
}

func (MemoryEditTool) Description() string {
	return "Creates, edits, or deletes memory files inside the agent's persistent memory directory. " +
		"Use action='insert' to insert content before a specific line number. " +
		"Use action='str_replace' to replace a unique exact string occurrence. " +
		"Use action='write' to completely overwrite or create a memory file. " +
		"Use action='delete' to remove a memory file or folder. " +
		"PREFER action='insert' or action='str_replace' over action='write' when modifying existing files."
}

func (MemoryEditTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"write", "insert", "str_replace", "delete"},
				"description": "Which edit operation to perform: 'write', 'insert', 'str_replace', or 'delete'.",
			},
			"path": map[string]any{
				"type":        []string{"string", "null"},
				"description": "Required for all actions: path to the target file or directory relative to memory directory.",
			},
			"content": map[string]any{
				"type": []string{"string", "null"},
				"description": "Required for action='write' (full content) and action='insert' (content to insert). " +
					"Ignored for action='str_replace' and action='delete'.",
			},
			"line": map[string]any{
				"type": []string{"integer", "null"},
				"description": "Required for action='insert': line number before which content is inserted " +
					"(1-based). Ignored otherwise.",
			},
			"old_str": map[string]any{
				"type": []string{"string", "null"},
				"description": "Required for action='str_replace': exact text string to replace. Must occur exactly once. " +
					"Ignored otherwise.",
			},
			"new_str": map[string]any{
				"type": []string{"string", "null"},
				"description": "Only used for action='str_replace': string to replace old_str with (can be empty). " +
					"Ignored otherwise.",
			},
		},
		"required":             []string{"action", "path", "content", "line", "old_str", "new_str"},
		"additionalProperties": false,
	}
}

func (m MemoryEditTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		agentName := strings.TrimSpace(handle.Name())
		if agentName == "" {
			return "", fmt.Errorf("memory_edit: agent handle returned empty name")
		}

		agentDir := filepath.Clean(filepath.Join(memoryBaseDir, filepath.Base(agentName)))

		var args struct {
			Action  string `json:"action"`
			Path    string `json:"path"`
			Content string `json:"content"`
			Line    int    `json:"line"`
			OldStr  string `json:"old_str"`
			NewStr  string `json:"new_str"`
		}
		if argumentsJSON != "" {
			if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
				return "", fmt.Errorf("memory_edit: invalid arguments: %w", err)
			}
		}

		if strings.TrimSpace(args.Path) == "" {
			return "", fmt.Errorf("memory_edit: 'path' is required for action=%q", args.Action)
		}

		switch args.Action {
		case "write":
			return memoryWrite(agentDir, args.Path, args.Content, m.maxBytes)
		case "insert":
			return memoryInsert(agentDir, args.Path, args.Line, args.Content, m.maxBytes)
		case "str_replace":
			return memoryStrReplace(agentDir, args.Path, args.OldStr, args.NewStr, m.maxBytes)
		case "delete":
			return memoryDelete(agentDir, args.Path)
		case "":
			return "", fmt.Errorf("memory_edit: 'action' is required (must be 'write', 'insert', 'str_replace', or 'delete')")
		default:
			return "", fmt.Errorf("memory_edit: unknown action %q (must be 'write', 'insert', 'str_replace', or 'delete')", args.Action)
		}
	}
}

func memoryWrite(agentDir string, path string, content string, maxBytes int) (string, error) {
	if maxBytes > 0 && len([]byte(content)) > maxBytes {
		return "", fmt.Errorf("memory_write: content size (%d bytes) exceeds maximum allowed limit of %d bytes", len([]byte(content)), maxBytes)
	}

	agentDirClean := filepath.Clean(agentDir)
	targetPath := filepath.Clean(filepath.Join(agentDirClean, path))

	if targetPath == agentDirClean || !strings.HasPrefix(targetPath, agentDirClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("memory_write: path %q is outside of agent memory directory", path)
	}

	parentDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return "", fmt.Errorf("memory_write: failed to create directories for %q: %w", targetPath, err)
	}

	if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("memory_write: failed to write file %q: %w", path, err)
	}

	relPath, err := filepath.Rel(agentDirClean, targetPath)
	if err != nil {
		relPath = path
	}

	out := struct {
		Path         string `json:"path"`
		BytesWritten int    `json:"bytes_written"`
		Status       string `json:"status"`
	}{
		Path:         relPath,
		BytesWritten: len([]byte(content)),
		Status:       "success",
	}

	result, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("memory_write: failed to marshal result: %w", err)
	}
	return string(result), nil
}

func memoryInsert(agentDir string, path string, line int, content string, maxBytes int) (string, error) {
	if line < 1 {
		return "", fmt.Errorf("memory_insert: line must be >= 1")
	}

	agentDirClean := filepath.Clean(agentDir)
	targetPath := filepath.Clean(filepath.Join(agentDirClean, path))

	if targetPath == agentDirClean || !strings.HasPrefix(targetPath, agentDirClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("memory_insert: path %q is outside of agent memory directory", path)
	}

	relPath, err := filepath.Rel(agentDirClean, targetPath)
	if err != nil {
		relPath = path
	}

	respond := func(linesAdded int) (string, error) {
		out := struct {
			Path               string `json:"path"`
			InsertedBeforeLine int    `json:"inserted_before_line"`
			LinesAdded         int    `json:"lines_added"`
		}{
			Path:               relPath,
			InsertedBeforeLine: line,
			LinesAdded:         linesAdded,
		}
		b, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("memory_insert: failed to marshal result: %w", err)
		}
		return string(b), nil
	}

	if content == "" {
		return respond(0)
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("memory_insert: file %q does not exist", path)
		}
		return "", fmt.Errorf("memory_insert: failed to read file %q: %w", path, err)
	}

	fileStr := string(data)
	hasTrailingNewline := strings.HasSuffix(fileStr, "\n")
	cleanStdout := strings.TrimSuffix(strings.ReplaceAll(fileStr, "\r\n", "\n"), "\n")

	var lines []string
	if cleanStdout != "" {
		lines = strings.Split(cleanStdout, "\n")
	}
	if line > len(lines)+1 {
		return "", fmt.Errorf("memory_insert: line %d out of range (file has %d lines)", line, len(lines))
	}

	insertContent := strings.TrimSuffix(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	insertLines := strings.Split(insertContent, "\n")

	idx := line - 1
	newLines := make([]string, 0, len(lines)+len(insertLines))
	newLines = append(newLines, lines[:idx]...)
	newLines = append(newLines, insertLines...)
	newLines = append(newLines, lines[idx:]...)
	newContent := strings.Join(newLines, "\n")
	if hasTrailingNewline || len(lines) == 0 {
		newContent += "\n"
	}

	if maxBytes > 0 && len([]byte(newContent)) > maxBytes {
		return "", fmt.Errorf("memory_insert: resulting file size (%d bytes) would exceed maximum allowed limit of %d bytes", len([]byte(newContent)), maxBytes)
	}

	if err := os.WriteFile(targetPath, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("memory_insert: failed to write file %q: %w", path, err)
	}

	return respond(len(insertLines))
}

func memoryStrReplace(agentDir string, path string, oldStr string, newStr string, maxBytes int) (string, error) {
	if oldStr == "" {
		return "", fmt.Errorf("memory_str_replace: 'old_str' must not be empty")
	}

	agentDirClean := filepath.Clean(agentDir)
	targetPath := filepath.Clean(filepath.Join(agentDirClean, path))

	if targetPath == agentDirClean || !strings.HasPrefix(targetPath, agentDirClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("memory_str_replace: path %q is outside of agent memory directory", path)
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("memory_str_replace: file %q does not exist", path)
		}
		return "", fmt.Errorf("memory_str_replace: failed to read file %q: %w", path, err)
	}

	fileStr := string(data)
	count := strings.Count(fileStr, oldStr)
	if count == 0 {
		return "", fmt.Errorf("memory_str_replace: old_str not found in %s", path)
	}
	if count > 1 {
		return "", fmt.Errorf("memory_str_replace: old_str is not unique in %s (found %d occurrences), add more context to make it unique", path, count)
	}

	newContent := strings.Replace(fileStr, oldStr, newStr, 1)

	if maxBytes > 0 && len([]byte(newContent)) > maxBytes {
		return "", fmt.Errorf("memory_str_replace: resulting file size (%d bytes) would exceed maximum allowed limit of %d bytes", len([]byte(newContent)), maxBytes)
	}

	if err := os.WriteFile(targetPath, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("memory_str_replace: failed to write file %q: %w", path, err)
	}

	relPath, err := filepath.Rel(agentDirClean, targetPath)
	if err != nil {
		relPath = path
	}

	out := struct {
		Path    string `json:"path"`
		Success bool   `json:"success"`
	}{
		Path:    relPath,
		Success: true,
	}

	result, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("memory_str_replace: failed to marshal result: %w", err)
	}
	return string(result), nil
}

func memoryDelete(agentDir string, path string) (string, error) {
	agentDirClean := filepath.Clean(agentDir)
	targetPath := filepath.Clean(filepath.Join(agentDirClean, path))

	if targetPath == agentDirClean || !strings.HasPrefix(targetPath, agentDirClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("memory_delete: path %q is invalid or outside of agent memory directory", path)
	}

	_, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("memory_delete: file or directory %q does not exist", path)
		}
		return "", fmt.Errorf("memory_delete: unable to access %q: %w", path, err)
	}

	if err := os.RemoveAll(targetPath); err != nil {
		return "", fmt.Errorf("memory_delete: failed to delete %q: %w", path, err)
	}

	relPath, err := filepath.Rel(agentDirClean, targetPath)
	if err != nil {
		relPath = path
	}

	out := struct {
		Path   string `json:"path"`
		Status string `json:"status"`
	}{
		Path:   relPath,
		Status: "deleted",
	}

	result, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("memory_delete: failed to marshal result: %w", err)
	}
	return string(result), nil
}
