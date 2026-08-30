
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

// FileEditTool bundles the three file-mutating operations - writing/
// overwriting a whole file, inserting text before a given line, and
// replacing a unique string occurrence - into a single tool, dispatched via
// the "action" parameter ("write", "insert", or "str_replace").
type FileEditTool struct {
	containerName string
	timeout       time.Duration
}

// NewFileEditTool constructs a FileEditTool, reading all relevant config
// values once up front.
func NewFileEditTool() FileEditTool {
	cfg := tool.GetToolConfig()
	containerName := config.ReadEntry(cfg, "sandbox.container_name", "ceres-sandbox")
	timeout, err := time.ParseDuration(config.ReadEntry(cfg, "sandbox.timeout", "120s"))
	if err != nil {
		panic(fmt.Errorf("file_edit: error while parsing sandbox.timeout in toolconfig.toml: %w", err))
	}

	return FileEditTool{
		containerName: containerName,
		timeout:       timeout,
	}
}

func (FileEditTool) Name() string {
	return "file_edit"
}

func (FileEditTool) Description() string {
	return "Writes, inserts into, or edits a file inside the sandbox container, depending on 'action'. " +
		"action='write': writes 'content' to the file, creating parent directories and the file itself if needed, " +
		"or overwriting it entirely if it already exists. Returns metadata about the resulting file (line count, " +
		"byte size, whether it ends with a trailing newline). " +
		"action='insert': inserts 'content' into the file before the given 'line' number. " +
		"action='str_replace': replaces a unique, exact occurrence of 'old_str' with 'new_str' in the file; " +
		"'old_str' must match the file's current content exactly (including whitespace/indentation) and occur " +
		"exactly once. " +
		"PREFER action='insert' or action='str_replace' over action='write' if possible, to avoid rewriting the " +
		"whole file. Use file_read first to see the current content and line numbers before inserting or replacing."
}

func (FileEditTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"write", "insert", "str_replace"},
				"description": "Which operation to perform: 'write' overwrites/creates the whole file, 'insert' " +
					"inserts content before a line, 'str_replace' replaces a unique string occurrence.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or working-directory-relative path of the file to edit inside the sandbox container.",
			},
			"content": map[string]any{
				"type": []string{"string", "null"},
				"description": "Required for action='write' (full content to write, replacing the entire file) and " +
					"action='insert' (content to insert). Ignored for action='str_replace'.",
			},
			"line": map[string]any{
				"type": []string{"integer", "null"},
				"description": "Required for action='insert': line number before which content is inserted " +
					"(must be 1 or greater). Ignored otherwise.",
			},
			"old_str": map[string]any{
				"type": []string{"string", "null"},
				"description": "Required for action='str_replace': the exact string to replace. Must match the " +
					"file's current content exactly and occur exactly once. Ignored otherwise.",
			},
			"new_str": map[string]any{
				"type": []string{"string", "null"},
				"description": "Only used for action='str_replace': the string to replace old_str with. Can be " +
					"empty to delete old_str. Ignored otherwise.",
			},
		},
		"required":             []string{"action", "path", "content", "line", "old_str", "new_str"},
		"additionalProperties": false,
	}
}

func (t FileEditTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args struct {
			Action  string `json:"action"`
			Path    string `json:"path"`
			Content string `json:"content"`
			Line    int    `json:"line"`
			OldStr  string `json:"old_str"`
			NewStr  string `json:"new_str"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("file_edit: invalid arguments: %w", err)
		}

		switch args.Action {
		case "write":
			return t.fileWrite(ctx, args.Path, args.Content)
		case "insert":
			return t.fileInsert(ctx, args.Path, args.Line, args.Content)
		case "str_replace":
			return t.fileStrReplace(ctx, args.Path, args.OldStr, args.NewStr)
		case "":
			return "", fmt.Errorf("file_edit: 'action' is required (must be 'write', 'insert' or 'str_replace')")
		default:
			return "", fmt.Errorf("file_edit: unknown action %q (must be 'write', 'insert' or 'str_replace')", args.Action)
		}
	}
}

// fileWrite writes content to a file inside the sandbox container, creating
// it (and any parent directories) if necessary, or overwriting it if it
// already exists. After writing, it returns useful metadata about the
// resulting file so the agent can immediately reason about line numbers for
// follow-up edits without needing a separate file_read call.
func (t FileEditTool) fileWrite(ctx context.Context, path, content string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file_write: path must not be empty")
	}

	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Ensure the parent directory exists, then write the content via a
	// heredoc fed to "cat > file". Using a quoted heredoc delimiter
	// ("EOF_FILE_WRITE") prevents the shell from expanding $variables,
	// backticks, etc. inside the content. We pick a delimiter unlikely to
	// collide with file content; if it somehow appears verbatim in the
	// content this would break, but that's an acceptable, well-understood
	// limitation of the heredoc approach.
	const delim = "CERES_FILE_WRITE_EOF"
	quotedPath := shellQuote(path)
	cmd := fmt.Sprintf(
		"mkdir -p -- \"$(dirname -- %s)\" && cat > %s <<'%s'\n%s\n%s",
		quotedPath, quotedPath, delim, content, delim,
	)
	stdout, stderr, exitCode, err := runInContainer(execCtx, getDockerClient(), t.containerName, cmd)
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
	statCtx, statCancel := context.WithTimeout(ctx, t.timeout)
	defer statCancel()
	statOut, statErr, statExit, err := runInContainer(statCtx, getDockerClient(), t.containerName, statCmd)
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
		Path            string `json:"path"`
		BytesWritten    int    `json:"bytes_written"`
		LineCount       int    `json:"line_count"`
		EndsWithNewline bool   `json:"ends_with_newline"`
	}{
		Path:            path,
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

// fileInsert inserts content into a file in the sandbox container before
// the specified line number.
func (t FileEditTool) fileInsert(ctx context.Context, path string, line int, content string) (string, error) {
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
	stdout, stderr, exitCode, err := runInContainer(execCtx, getDockerClient(), t.containerName, readCmd)
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
	_, stderr, exitCode, err = runInContainer(execCtx, getDockerClient(), t.containerName, writeCmd)
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		return "", fmt.Errorf("file_insert write failed: %s", strings.TrimSpace(stderr))
	}

	return respond(len(insertLines))
}

// fileStrReplace replaces a unique occurrence of a string inside a file in
// the sandbox container. The old string must match the file's current
// content exactly and appear exactly once; this avoids ambiguous edits and
// the line-shifting issues that come with line-number-based replacements.
func (t FileEditTool) fileStrReplace(ctx context.Context, path, oldStr, newStr string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file_str_replace: path must not be empty")
	}
	if oldStr == "" {
		return "", fmt.Errorf("file_str_replace: old_str must not be empty")
	}

	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	cli := getDockerClient()

	// Read the current file content.
	readCmd := fmt.Sprintf("cat -- %s", shellQuote(path))
	stdout, stderr, exitCode, err := runInContainer(execCtx, cli, t.containerName, readCmd)
	if err != nil {
		return "", fmt.Errorf("file_str_replace: failed to read file from sandbox: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("file_str_replace: cat exited with code %d: %s", exitCode, strings.TrimSpace(stderr))
	}

	// Ensure old_str occurs exactly once, so the replacement is unambiguous.
	count := strings.Count(stdout, oldStr)
	if count == 0 {
		return "", fmt.Errorf("file_str_replace: old_str not found in %s", path)
	}
	if count > 1 {
		return "", fmt.Errorf("file_str_replace: old_str is not unique in %s (found %d occurrences), add more context to make it unique", path, count)
	}
	newContent := strings.Replace(stdout, oldStr, newStr, 1)

	// Write the new content back using a heredoc with a random-ish
	// delimiter to avoid clashing with content that itself contains
	// "EOF"-like markers.
	writeCmd := fmt.Sprintf("cat > %s <<'CERES_EOF_MARKER'\n%s\nCERES_EOF_MARKER", shellQuote(path), newContent)
	_, stderr, exitCode, err = runInContainer(execCtx, cli, t.containerName, writeCmd)
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

// parseStatOutput parses the three-line output produced by fileWrite's stat
// command:
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
