// Package web implements the browser platform. It serves an embedded layout
// and streams an agent's history and live tokens over a WebSocket.
package web

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/stream"
	"github.com/alex0esc/ceres/pkg/appconfig"
	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/gorilla/websocket"
)

//go:embed static
var staticFS embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host
	},
}

type Web struct {
	addr           string
	defaultAgent   string
	showReasoning  bool
	messageTimeout time.Duration

	mu     sync.Mutex
	server *http.Server
	agents map[string]handles.AgentHandle
}

// NewWeb reads the web settings once up front. Missing keys are written to the
// app config by ReadEntry.
func NewWeb() *Web {
	cfg := appconfig.Get()
	return &Web{
		addr:           config.ReadEntry(cfg, "web.addr", "127.0.0.1:8080"),
		defaultAgent:   config.ReadEntry(cfg, "web.default_agent", ""),
		showReasoning:  config.ReadEntry(cfg, "web.show_reasoning", true),
		messageTimeout: config.ReadEntry(cfg, "web.message_timeout", time.Hour),
	}
}

func (w *Web) Name() string {
	return "web"
}

// Listen serves the embedded site until StopListen is called.
func (w *Web) Listen(agents map[string]handles.AgentHandle) {
	assets, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Printf("web: failed to open embedded assets: %v", err)
		return
	}

	w.mu.Lock()
	w.agents = agents
	w.mu.Unlock()

	srv := &http.Server{Addr: w.addr, Handler: w.handler(assets)}

	w.mu.Lock()
	w.server = srv
	w.mu.Unlock()

	log.Printf("web: listening on %s", w.addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("web: server stopped: %v", err)
	}
}

// handler builds the HTTP routes for the embedded site and the WebSocket.
func (w *Web) handler(assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(rw http.ResponseWriter, _ *http.Request) {
		data, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			http.Error(rw, "index.html not found", http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = rw.Write(data)
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("GET /ws", w.handleWebSocket)
	mux.HandleFunc("GET /api/agents", w.handleAgents)
	mux.HandleFunc("GET /api/commands", w.handleCommands)
	mux.HandleFunc("GET /api/status", w.handleStatus)
	mux.HandleFunc("POST /api/message", w.handleMessage)
	return mux
}

func (w *Web) StopListen() {
	w.mu.Lock()
	srv := w.server
	w.mu.Unlock()
	if srv == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("web: shutdown failed: %v", err)
	}

	w.mu.Lock()
	w.server = nil
	w.mu.Unlock()
}

// agentHandle resolves an agent for one browser session. A name requested by
// the client wins, then the configured default, then the alphabetically first
// agent.
func (w *Web) agentHandle(name string) (handles.AgentHandle, bool) {
	w.mu.Lock()
	agents := w.agents
	fallback := w.defaultAgent
	w.mu.Unlock()

	if name != "" {
		agnt, ok := agents[name]
		return agnt, ok
	}
	if fallback != "" {
		agnt, ok := agents[fallback]
		return agnt, ok
	}

	names := make([]string, 0, len(agents))
	for n := range agents {
		names = append(names, n)
	}
	if len(names) == 0 {
		return nil, false
	}
	slices.Sort(names)
	return agents[names[0]], true
}

// handleWebSocket streams a fresh snapshot followed by live tokens to one
// browser. A single goroutine owns all writes to the connection.
func (w *Web) handleWebSocket(rw http.ResponseWriter, req *http.Request) {
	conn, err := upgrader.Upgrade(rw, req, nil)
	if err != nil {
		log.Printf("web: websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	agnt, ok := w.agentHandle(req.URL.Query().Get("agent"))
	if !ok {
		_ = conn.WriteJSON(frame{Type: frameError, Message: "no agent available"})
		return
	}

	client := agnt.ClientHandle()
	snap, sub := client.SnapshotAndSubscribe(stream.DefaultBuffer)
	defer sub.Close()

	if err := conn.WriteJSON(snapshotFrame(agnt.Name(), snap, w.showReasoning)); err != nil {
		return
	}

	// Reading is only needed to notice client disconnects; the browser does not
	// send application messages yet.
	done := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(done) }) }
	defer closeDone()
	go func() {
		defer closeDone()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case token, ok := <-sub.Tokens():
			if !ok {
				if errors.Is(sub.Err(), stream.ErrOverflow) {
					_ = conn.WriteJSON(frame{Type: frameReset})
				}
				return
			}
			if token.Type == history.TokenTypeResetChat {
				// Compression replaced the history in one critical section before
				// publishing this token. Clearing alone would leave an empty chat,
				// so follow the reset with a fresh snapshot that rebuilds it from
				// the compressed history (and any partial answer still streaming).
				if err := conn.WriteJSON(frame{Type: frameReset}); err != nil {
					return
				}
				snap := client.Snapshot()
				if err := conn.WriteJSON(snapshotFrame(agnt.Name(), snap, w.showReasoning)); err != nil {
					return
				}
				continue
			}

			f, send := tokenFrame(token, w.showReasoning)
			if !send {
				continue
			}
			if err := conn.WriteJSON(f); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
