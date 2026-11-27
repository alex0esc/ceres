package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/stream"
	"github.com/alex0esc/ceres/pkg/command"
)

func TestSmoke(t *testing.T) {
	assets, err := fs.Sub(staticFS, "static")
	if err != nil {
		t.Fatal(err)
	}
	handler := (&Web{}).handler(assets)

	paths := []string{
		"/", "/static/util.js", "/static/render.js", "/static/image.js",
		"/static/agents.js", "/static/commands.js", "/static/stream.js",
		"/static/connection.js",
		"/static/app.js", "/static/style.css",
		"/static/vendor/marked.min.js", "/static/vendor/purify.min.js",
		"/static/vendor/highlight.min.js", "/static/vendor/github-dark.min.css",
		"/static/vendor/katex.min.js", "/static/vendor/auto-render.min.js",
		"/static/vendor/katex.min.css", "/static/vendor/fonts/KaTeX_Main-Regular.woff2",
		"/static/vendor/fonts/KaTeX_Main-Regular.woff",
		"/api/commands", "/api/agents",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rw := httptest.NewRecorder()
		handler.ServeHTTP(rw, req)
		if rw.Code != http.StatusOK {
			t.Errorf("%s: status %d", path, rw.Code)
		}
	}
}

func TestSnapshotFrame(t *testing.T) {
	snap := stream.Snapshot{
		History: history.History{Entries: []history.Entry{
			{Type: history.EntryTypeUser, Text: "hi"},
			{Type: history.EntryTypeAssistant, Text: "**hello**"},
			{Type: history.EntryTypeReasoning, Text: "hidden"},
		}},
		Partial: []history.Token{{Type: history.EntryTypeAssistant, Text: "part"}},
	}

	f := snapshotFrame("Ceres", snap, false)
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out["type"] != "snapshot" || out["agent"] != "Ceres" {
		t.Fatalf("unexpected frame: %s", data)
	}
	entries := out["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %s", len(entries), data)
	}
	if !strings.Contains(string(data), `"text":"**hello**"`) {
		t.Fatalf("markdown text missing: %s", data)
	}
}

func TestMessageImageValidation(t *testing.T) {
	handler := (&Web{}).handler(nil)

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/message", strings.NewReader(body))
		rw := httptest.NewRecorder()
		handler.ServeHTTP(rw, req)
		return rw.Code
	}

	if code := post(`{"text":"hi","images":[{"base64":"!!!","mime_type":"image/png"}]}`); code != http.StatusBadRequest {
		t.Fatalf("invalid base64: got %d, want 400", code)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	valid := fmt.Sprintf(`{"text":"hi","images":[{"base64":%q,"mime_type":"image/png"}]}`,
		base64.StdEncoding.EncodeToString(buf.Bytes()))
	// No agent is registered in this handler, so a valid image must get past
	// validation and fail later at agent resolution with 503.
	if code := post(valid); code != http.StatusServiceUnavailable {
		t.Fatalf("valid image: got %d, want 503", code)
	}
}

func TestCommandInfos(t *testing.T) {
	cmds := commandInfos([]command.Command{
		{Name: "clear", Description: "Clear history."},
		{
			Name:        "wakeup",
			Description: "Manage wakeups.",
			Subcommands: []command.Command{
				{Name: "list", Description: "List wakeups."},
				{Name: "run", Description: "Run a wakeup."},
			},
		},
	})

	data, err := json.Marshal(cmds)
	if err != nil {
		t.Fatal(err)
	}
	// Leaf commands must omit the key entirely so the browser sees no empty
	// subcommand list.
	if strings.Contains(string(data), `"subcommands":[]`) || strings.Contains(string(data), `"subcommands":null`) {
		t.Fatalf("leaf command has a subcommands key: %s", data)
	}
	if len(cmds) != 2 || len(cmds[1].Subcommands) != 2 || cmds[1].Subcommands[0].Name != "list" {
		t.Fatalf("unexpected command tree: %+v", cmds)
	}
	if !strings.Contains(string(data), `"description":"List wakeups."`) {
		t.Fatalf("subcommand description missing: %s", data)
	}
}
