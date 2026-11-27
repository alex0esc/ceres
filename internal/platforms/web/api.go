package web

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/internal/tools"
	"github.com/alex0esc/ceres/pkg/command"
)

// maxMessageBody caps a single POST so a huge image cannot exhaust memory.
const maxMessageBody = 32 << 20 // 32 MiB

type messageImage struct {
	Base64   string `json:"base64"`
	MimeType string `json:"mime_type"`
}

type messageRequest struct {
	Text   string         `json:"text"`
	Agent  string         `json:"agent"`
	Images []messageImage `json:"images"`
}

type agentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	State       string `json:"state"`
}

// commandInfo is the JSON-safe view of command.Command. Handlers are functions
// and cannot be serialized, so only names, descriptions and the nested command
// tree are sent; that is everything the browser autocomplete needs.
type commandInfo struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Subcommands []commandInfo `json:"subcommands,omitempty"`
}

func commandInfos(cmds []command.Command) []commandInfo {
	if len(cmds) == 0 {
		return nil
	}
	out := make([]commandInfo, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, commandInfo{
			Name:        c.Name,
			Description: c.Description,
			Subcommands: commandInfos(c.Subcommands),
		})
	}
	return out
}

// handleCommands exposes the registered command tree so the browser can build
// slash-command autocomplete without duplicating any command definitions.
func (w *Web) handleCommands(rw http.ResponseWriter, _ *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]any{
		"commands": commandInfos(command.All()),
	})
}

// handleStatus reports the selected agent's live token usage and compression
// state. The browser polls it to keep the header counter and working indicator
// in sync without waiting for a fresh WebSocket snapshot.
func (w *Web) handleStatus(rw http.ResponseWriter, req *http.Request) {
	agnt, ok := w.agentHandle(req.URL.Query().Get("agent"))
	if !ok {
		http.Error(rw, "no agent available", http.StatusServiceUnavailable)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]any{
		"tokens":      agnt.ClientHandle().TotalTokens(),
		"compressing": agnt.ClientHandle().IsCompressing(),
		"state":       strings.ToLower(agnt.State().String()),
	})
}

// handleAgents lists all available agents in a stable order so the sidebar can
// show them. "current" marks the agent a fresh WebSocket would attach to.
func (w *Web) handleAgents(rw http.ResponseWriter, _ *http.Request) {
	w.mu.Lock()
	agents := w.agents
	w.mu.Unlock()

	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	slices.Sort(names)

	infos := make([]agentInfo, 0, len(names))
	for _, name := range names {
		agnt := agents[name]
		infos = append(infos, agentInfo{
			Name:        agnt.Name(),
			Description: agnt.Description(),
			State:       agnt.State().String(),
		})
	}

	current := ""
	if agnt, ok := w.agentHandle(""); ok {
		current = agnt.Name()
	}

	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]any{
		"agents":  infos,
		"current": current,
	})
}

// handleMessage accepts a prompt from the browser and forwards it to the
// selected agent. The reply itself arrives asynchronously over the WebSocket,
// just like tokens triggered by any other platform. Slash commands are handled
// synchronously and their result is returned in the response.
func (w *Web) handleMessage(rw http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(rw, req.Body, maxMessageBody)
	var body messageRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(rw, "invalid json body", http.StatusBadRequest)
		return
	}

	text := strings.TrimSpace(body.Text)

	// Decode and validate every image before submitting the task. The mime type
	// is sniffed from the bytes, exactly like Discord's attachment handler does,
	// so a client-supplied type cannot smuggle non-images into the history.
	images := make([]task.ImageInput, 0, len(body.Images))
	for _, img := range body.Images {
		data, err := base64.StdEncoding.DecodeString(img.Base64)
		if err != nil {
			http.Error(rw, "invalid image data", http.StatusBadRequest)
			return
		}
		mimeType := tools.DetectImageMimeType("", data)
		if mimeType == "" {
			http.Error(rw, "unsupported image type", http.StatusBadRequest)
			return
		}
		images = append(images, task.ImageInput{Base64Image: img.Base64, MimeType: mimeType})
	}

	if text == "" && len(images) == 0 {
		http.Error(rw, "empty message", http.StatusBadRequest)
		return
	}

	agnt, ok := w.agentHandle(body.Agent)
	if !ok {
		http.Error(rw, "no agent available", http.StatusServiceUnavailable)
		return
	}

	if handled, msg := command.CheckCommand(agnt, text); handled {
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]string{"command_result": msg})
		return
	}

	tsk := task.TaskAskSingle(task.Prompt{Text: text, Images: images}, w.messageTimeout)
	result := agnt.SubmitTask(tsk)
	go func() {
		if err := (<-result).Err; err != nil {
			log.Printf("web: error while processing message: %v", err)
		}
	}()

	rw.WriteHeader(http.StatusNoContent)
}
