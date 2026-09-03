
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
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
)

// ViewImageTool reads an image from the sandbox container and appends it
// to the agent's chat history as a base64-encoded image.
type ViewImageTool struct {
	containerName string
	timeout       time.Duration
	maxSize       int64
}

// NewViewImageTool constructs a ViewImageTool, reading all relevant config
// values once up front.
func NewViewImageTool() ViewImageTool {
	cfg := tool.GetToolConfig()

	containerName := config.ReadEntry(cfg, "sandbox.container_name", "ceres-sandbox")

	timeout, err := time.ParseDuration(config.ReadEntry(cfg, "sandbox.timeout", "120s"))
	if err != nil {
		panic(fmt.Errorf("view_image: error while parsing sandbox.timeout in toolconfig.toml: %w", err))
	}
	if timeout <= 0 {
		panic(fmt.Errorf("view_image: sandbox.timeout must be positive"))
	}

	var defSize int64 = 4096
	maxSize := config.ReadEntry(cfg, "view_image.max_size_kb", defSize) * 1024

	return ViewImageTool{
		containerName: containerName,
		timeout:       timeout,
		maxSize:       maxSize,
	}
}

func (ViewImageTool) Name() string {
	return "view_image"
}

func (t ViewImageTool) Description() string {
	return fmt.Sprintf(
		"Reads an image from the sandbox container and appends it as user message in the chat.\n"+
			"The path may be absolute or relative to the sandbox working directory. Images larger than %d bytes are rejected.",
		t.maxSize,
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

func (t ViewImageTool) Handler() tool.ToolHandler {
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

		imageData, size, err := readFileFromContainer(ctx, t.containerName, t.timeout, t.maxSize, args.Path)
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

		handle.ClientHandle().AppendUserPrompt(handles.Prompt{
			Text: "",
			Images: []handles.ImageInput{{
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
