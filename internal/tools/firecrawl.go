package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/tool"
)

// WebExtractTool sends a scrape request to the Firecrawl API and returns the
// extracted page content (as Markdown) to the agent.
type WebExtractTool struct {
	// HTTPClient allows overriding the client (useful for tests). Defaults to
	// a client with a 60s timeout if nil (crawls can take a while).
	HTTPClient *http.Client
}

type webExtractArgs struct {
	URL       string `json:"url"`
	OnlyText  bool   `json:"only_main_content"`
	MaxLength int    `json:"max_length"`
}

// firecrawlScrapeRequest models the request body for POST /v1/scrape.
type firecrawlScrapeRequest struct {
	URL             string   `json:"url"`
	Formats         []string `json:"formats"`
	OnlyMainContent bool     `json:"onlyMainContent"`
}

// firecrawlScrapeResponse models the subset of the Firecrawl response we care about.
type firecrawlScrapeResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Markdown string `json:"markdown"`
		Metadata struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			SourceURL   string `json:"sourceURL"`
			StatusCode  int    `json:"statusCode"`
		} `json:"metadata"`
	} `json:"data"`
	Error string `json:"error"`
}

func (t *WebExtractTool) Name() string {
	return "web_extract"
}

func (t *WebExtractTool) Description() string {
	return "Fetches a URL via Firecrawl and returns the extracted page content as clean Markdown. " +
		"IMPORTANT: If info is missing from the result, don't guess it -- extract another page or tell the user it's missing!"
}

func (t *WebExtractTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL of the page to extract content from.",
			},
			"only_main_content": map[string]any{
				"type":        "boolean",
				"description": "If true (default), strips navigation, ads, and boilerplate, keeping only the main article/page content.",
			},
			"max_length": map[string]any{
				"type":        "integer",
				"description": "Optional. Truncate the returned Markdown to roughly this many characters (default: no truncation).",
			},
		},
		"required": []string{"url"},
		"additionalProperties": false,
	}
}

func (t *WebExtractTool) apiKey() string {
	apiKey := config.ReadEntry(tool.GetToolConfig(), "firecrawl.api_key", "")
	return apiKey
}

func (t *WebExtractTool) baseURL() string {
	url := config.ReadEntry(tool.GetToolConfig(), "firecrawl.url", "http://localhost:3002")
	return strings.TrimRight(url, "/")
}

func (t *WebExtractTool) client() *http.Client {
	if t.HTTPClient != nil {
		return t.HTTPClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (t *WebExtractTool) Handler() tool.ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args webExtractArgs
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		if strings.TrimSpace(args.URL) == "" {
			return "", fmt.Errorf("url must not be empty")
		}

		// Default to true unless explicitly present as false. Since Go's zero
		// value for bool is false, we treat the JSON field as "opt-out" by
		// re-parsing into a map to detect presence.
		onlyMain := true
		var raw map[string]any
		if err := json.Unmarshal([]byte(argumentsJSON), &raw); err == nil {
			if v, ok := raw["only_main_content"].(bool); ok {
				onlyMain = v
			}
		}

		reqBody := firecrawlScrapeRequest{
			URL:             args.URL,
			Formats:         []string{"markdown"},
			OnlyMainContent: onlyMain,
		}
		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return "", fmt.Errorf("failed to build request body: %w", err)
		}

		endpoint := t.baseURL() + "/v1/scrape"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return "", fmt.Errorf("failed to build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+t.apiKey())

		resp, err := t.client().Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to reach firecrawl api: %w", err)
		}
		defer resp.Body.Close()

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read firecrawl response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("firecrawl returned status %s: %s", resp.Status, truncateStr(string(respBytes), 300))
		}

		var parsed firecrawlScrapeResponse
		if err := json.Unmarshal(respBytes, &parsed); err != nil {
			return "", fmt.Errorf("failed to parse firecrawl response: %w", err)
		}

		if !parsed.Success {
			msg := parsed.Error
			if msg == "" {
				msg = "unknown error"
			}
			return "", fmt.Errorf("firecrawl scrape failed: %s", msg)
		}

		content := parsed.Data.Markdown
		truncated := false
		if args.MaxLength > 0 && len(content) > args.MaxLength {
			content = content[:args.MaxLength]
			truncated = true
		}

		var sb strings.Builder
		if parsed.Data.Metadata.Title != "" {
			sb.WriteString("Title: ")
			sb.WriteString(parsed.Data.Metadata.Title)
			sb.WriteString("\n")
		}
		if parsed.Data.Metadata.Description != "" {
			sb.WriteString("Description: ")
			sb.WriteString(parsed.Data.Metadata.Description)
			sb.WriteString("\n")
		}
		sb.WriteString("Source: ")
		sb.WriteString(args.URL)
		sb.WriteString("\n\n")
		sb.WriteString(content)
		if truncated {
			sb.WriteString("\n\n...[truncated]")
		}

		// Short reminder placed right next to the extracted data, kept
		// minimal to avoid wasting context tokens.
		sb.WriteString("\n\n[If info is missing above, don't guess -- extract another page or say it's missing.]")

		return sb.String(), nil
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func init() {
	tool.Register(&WebExtractTool{})
}
