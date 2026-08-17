package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// ViewImageTool reads an image from the sandbox container and appends it
// to the agent's chat history as a base64-encoded image.
type ViewImageTool struct{}

func (ViewImageTool) Name() string {
	return "view_image"
}

func (ViewImageTool) Description() string {
	var def_size int64 = 4096
	maxSize := config.ReadEntry(tool.GetToolConfig(), "view_image.max_size_kb", def_size) * 1024
	return fmt.Sprintf(
		"Reads an image from the sandbox container and appends it as user message in the chat.\n"+
		"The path may be absolute or relative to the sandbox working directory. Images larger than %d bytes are rejected.",
		maxSize,
	)
}

func (ViewImageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or working-directory-relative path of the image inside the sandbox container.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func (ViewImageTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string, handle handles.AgentHandle) (string, error) {
		var args struct {
			Path string `json:"path"`
		}

		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("view_image: invalid arguments: %w", err)
		}

		if strings.TrimSpace(args.Path) == "" {
			return "", fmt.Errorf("view_image: path must not be empty")
		}

		containerName := config.ReadEntry(
			tool.GetToolConfig(),
			"sandbox.container_name",
			"ceres-sandbox",
		)

		// Use the same timeout configured for sandbox commands.
		timeout, err := time.ParseDuration(
			config.ReadEntry(
				tool.GetToolConfig(),
				"sandbox.bash.timeout",
				"60s",
			),
		)
		if err != nil {
			return "", fmt.Errorf(
				"view_image: error while parsing sandbox.bash.timeout in toolconfig.toml: %w",
				err,
			)
		}
		if timeout <= 0 {
			return "", fmt.Errorf("view_image: sandbox.bash.timeout must be positive")
		}

		var def_size int64 = 4096
		maxSize := config.ReadEntry(tool.GetToolConfig(), "view_image.max_size_kb", def_size) * 1024

		cli := getDockerClient()

		statCtx, statCancel := context.WithTimeout(ctx, timeout+2*killAfterBuffer)
		statCmd := fmt.Sprintf(
			"stat -c %%s -- %s",
			shellQuote(args.Path),
		)

		statOut, statErr, statExit, err := runInContainer(
			statCtx,
			cli,
			containerName,
			statCmd,
		)
		statCancel()

		if err != nil {
			return "", fmt.Errorf("view_image: failed to stat file in sandbox: %w", err)
		}

		if statExit != 0 {
			return "", fmt.Errorf(
				"view_image: stat exited with code %d: %s",
				statExit,
				strings.TrimSpace(statErr),
			)
		}

		size, err := strconv.ParseInt(strings.TrimSpace(statOut), 10, 64)
		if err != nil {
			return "", fmt.Errorf(
				"view_image: failed to parse file size %q: %w",
				strings.TrimSpace(statOut),
				err,
			)
		}

		if size <= 0 {
			return "", fmt.Errorf("view_image: image %q is empty", args.Path)
		}

		if size > maxSize {
			return "", fmt.Errorf(
				"view_image: image is %d bytes, which exceeds the configured limit of %d bytes",
				size,
				maxSize,
			)
		}

		// Read and base64-encode the image inside the sandbox.
		execCtx, cancel := context.WithTimeout(ctx, timeout+2*killAfterBuffer)
		defer cancel()

		cmd := fmt.Sprintf(
			"base64 -w 0 -- %s",
			shellQuote(args.Path),
		)

		stdout, stderr, exitCode, err := runInContainer(
			execCtx,
			cli,
			containerName,
			cmd,
		)
		if err != nil {
			return "", fmt.Errorf("view_image: failed to read image from sandbox: %w", err)
		}

		if exitCode != 0 {
			return "", fmt.Errorf(
				"view_image: base64 exited with code %d: %s",
				exitCode,
				strings.TrimSpace(stderr),
			)
		}

		base64Image := strings.TrimSpace(stdout)
		if base64Image == "" {
			return "", fmt.Errorf("view_image: image %q produced empty base64 data", args.Path)
		}


		imageData, err := base64.StdEncoding.DecodeString(base64Image)
		if err != nil {
			return "", fmt.Errorf("view_image: failed to decode image data: %w", err)
		}

		mimeType := detectImageMimeType(args.Path, imageData)
		if mimeType == "" {
			return "", fmt.Errorf(
				"view_image: could not determine a supported image MIME type for %q",
				args.Path,
			)
		}

		handle.ClientHandle().AppendImage(base64.StdEncoding.EncodeToString(imageData), mimeType, "")

		out := struct {
			Path     string `json:"path"`
			MimeType string `json:"mime_type"`
			Size     int64  `json:"size"`
		}{
			Path:     args.Path,
			MimeType: mimeType,
			Size:     size,
		}

		result, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("view_image: failed to marshal result: %w", err)
		}

		return string(result), nil
	}
}

func detectImageMimeType(path string, data []byte) string {
	detected := http.DetectContentType(data)
	if strings.HasPrefix(detected, "image/") {
		return detected
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" {
		if mimeType := mime.TypeByExtension(ext); strings.HasPrefix(mimeType, "image/") {
			return mimeType
		}

		switch ext {
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".png":
			return "image/png"
		case ".gif":
			return "image/gif"
		case ".webp":
			return "image/webp"
		case ".bmp":
			return "image/bmp"
		case ".tif", ".tiff":
			return "image/tiff"
		}
	}

	return ""
}


func init() {
	tool.Register(ViewImageTool{})
}
