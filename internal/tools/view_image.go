package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// ViewImageTool reads an image from the sandbox container and appends it
// to the agent's chat history as a base64-encoded image.
type ViewImageTool struct{}

// NewViewImageTool constructs a ViewImageTool. The maximum image size is
// configured per agent and read in the handler.
func NewViewImageTool() ViewImageTool {
	return ViewImageTool{}
}

func (ViewImageTool) Name() string {
	return "view_image"
}

func (ViewImageTool) Description() string {
	return "Reads an image from the sandbox container and appends it as user message in the chat.\n" +
		"The path may be absolute or relative to the sandbox working directory. A maximum image size applies, configured per agent (tool_settings.view_image.max_size_kb)."
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

		var defSize int64 = 8192
		maxSize := config.ReadEntry(handle.Config(), "tool_settings.view_image.max_size_kb", defSize) * 1024

		imageData, size, err := readFileFromContainer(ctx, handle.SandboxContainer(), sandboxOpTimeout, maxSize, args.Path)
		if err != nil {
			return "", fmt.Errorf("view_image: %w", err)
		}

		mimeType := DetectImageMimeType(args.Path, imageData)
		if mimeType == "" {
			return "", fmt.Errorf(
				"view_image: could not determine a supported image MIME type for %q",
				args.Path,
			)
		}

		handle.ClientHandle().AppendUserPrompt(task.Prompt{
			Text: "",
			Images: []task.ImageInput{{
				MimeType:    mimeType,
				Base64Image: base64.StdEncoding.EncodeToString(imageData),
			}},
		})

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

func DetectImageMimeType(path string, data []byte) string {
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
