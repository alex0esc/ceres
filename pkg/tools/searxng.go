package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alex0esc/ceres/pkg/config"
)

// SearxngTool queries a SearXNG instance and returns the results to the agent.
type SearxngTool struct {
	// HTTPClient allows overriding the client (useful for tests). Defaults to
	// a client with a 10s timeout if nil.
	HTTPClient *http.Client
}

type searxngArgs struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

// searxngResponse models the subset of the SearXNG JSON API we care about.
type searxngResponse struct {
	Query   string `json:"query"`
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
		Engine  string `json:"engine"`
	} `json:"results"`
}

func (t *SearxngTool) Name() string {
	return "web_search"
}

func (t *SearxngTool) Description() string {
	return "Searches the web via a SearXNG instance and returns a list of results (title, url, snippet) for the given query." +
		"IMPORTANT: If using the tool avoid multiple small and simular sounding request, instead increase the number of max_results!" +
		"Before doing the next search first check the current Results with web_extract and reason if you really need another search!" 
}

func (t *SearxngTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query to send to SearXNG.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default 5, max 20).",
			},
		},
		"required": []string{"query"},
		"additionalProperties": false,		
	}
}

func (t *SearxngTool) baseURL() string {
	url := config.ReadEntry(GetToolConfig(), "searxng.url", "http://localhost:8080")
	return url
}

func (t *SearxngTool) client() *http.Client {
	if t.HTTPClient != nil {
		return t.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (t *SearxngTool) Handler() ToolHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		var args searxngArgs
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		if strings.TrimSpace(args.Query) == "" {
			return "", fmt.Errorf("query must not be empty")
		}

		base := t.baseURL()
		if base == "" {
			return "", fmt.Errorf("no SearXNG base URL configured (set SearxngTool.BaseURL or SEARXNG_URL)")
		}

		limit := args.MaxResults
		if limit <= 0 {
			limit = 5
		}
		if limit > 20 {
			limit = 20
		}

		reqURL := strings.TrimRight(base, "/") + "/search?" + url.Values{
			"q":      {args.Query},
			"format": {"json"},
		}.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return "", fmt.Errorf("failed to build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "agent-tool/searxng")

		resp, err := t.client().Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to reach searxng instance: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read searxng response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("searxng returned status %s: %s", resp.Status, truncate(string(body), 300))
		}

		var parsed searxngResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return "", fmt.Errorf("failed to parse searxng response: %w", err)
		}

		if len(parsed.Results) == 0 {
			return fmt.Sprintf("No results found for query: %s", args.Query), nil
		}

		var sb strings.Builder
		sb.WriteString("Search results for \"");sb.WriteString(args.Query);sb.WriteString("\":\n\n")
		for i, r := range parsed.Results {
			if i >= limit {
				break
			}
			sb.WriteString(strconv.Itoa(i + 1));sb.WriteString(". ");sb.WriteString(r.Title);sb.WriteString("\n")
			sb.WriteString("   URL: ");sb.WriteString(r.URL);sb.WriteString("\n")
			if r.Content != "" {
				sb.WriteString("   ");sb.WriteString(r.Content);sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}

		return sb.String(), nil
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func init() {
	Register(&SearxngTool{})
}
