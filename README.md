# Ceres

Ceres is a self-hosted, multi-agent AI assistant platform written in Go. It runs one or more LLM agents (defined in TOML files) that can talk to you through a terminal UI (TUI) and/or Discord, use tools (shell, files, web, images), delegate work to subagents, keep persistent per-agent memory, and schedule future wakeups — all against any OpenAI-compatible endpoint (e.g. local llama.cpp or Ollama instances).

```
            ┌────────────────────────────────────────────────────────────┐
            │                        Ceres (Go)                          │
            │                                                            │
  Discord ──┤  platforms ──► agents ──► inference client ──► LLM endpoint │
    (DM)    │      ▲           │  (queue + worker,        (OpenAI        │
            │      │           │   one task at a time)     Responses API) │
  TUI  ─────┤  commands        │                                        │
  (bubbletea)     │            ├──► tools ──► Docker sandbox container   │
            │      │            │            (bash, python, files, ...)  │
            │      └────────────┤                                        │
            │                   ├──► subagents (other agents)            │
            │                   ├──► wakeups (cron / one-shot timers)    │
            │                   └──► memory (per-agent file store)       │
            └────────────────────────────────────────────────────────────┘
```

## Features

- **Multi-agent setup** — agents are plain TOML files in `agents/`. One main agent plus any number of subagents. Each agent has its own model, system prompt, tool set, and limits.
- **Tool system** — built-in tools for shell execution, Python snippets, sandbox file read/edit, web search (SearXNG), web extraction (Firecrawl), image viewing, time, Discord operations, memory, and wakeups. External tools can be registered via `pkg/tool`.
- **Docker sandbox** — all code-execution and file tools run inside a separate, isolated container (`ceres-sandbox`) via the Docker exec API, with per-command timeouts and hard kill-after escalation.
- **Streaming chat** — token-level streaming to a full-screen Bubble Tea TUI (agent list, markdown rendering, reasoning display, live token/agent status) and to Discord DMs (typing indicator, message chunking, image attachments).
- **Subagents** — the main agent can delegate tasks to subagents in parallel via the `subagent` tool. Each task runs in a fresh context and only the final summary (guided by `summary_instructions`) is returned to the caller. Agent-call cycles are detected and rejected.
- **Wakeups** — per-agent scheduled tasks: one-shot (`fire_at`) or recurring (`cron_spec`). When a wakeup fires, the agent's history is cleared and the wakeup's prompt chain runs in order in that fresh context. "Protected" wakeups can be created by the system/user but not removed by the agent.
- **Persistent memory** — each agent owns a `memories/<AgentName>/` directory with read/edit/delete tools (size-limited writes).
- **Context compression** — when the conversation's token count crosses a configurable threshold, older messages are summarized into a single summary message (tool-less LLM call) while the most recent messages are kept verbatim.
- **Reasoning support** — model reasoning output (raw or summarized, depending on backend) is kept in history for context and optionally rendered in the TUI.
- **Slash commands** — `/help`, `/clr`, `/cmp`, `/itr`, `/wake list|run <name>` work in both the TUI and Discord.
- **Hot reload** — type `reload` in the console to restart the app (configs, agents, platforms, wakeups) without re-running the binary; `tui` toggles the TUI.
- **Self-provisioning configs** — missing config files/agents/endpoints are created with sensible defaults on first start.

## Project layout

```
.
├── main.go                  # entrypoint: logging, app lifecycle, console loop (tui / reload / exit)
├── go.mod / go.sum
├── Dockerfile               # multi-stage build (golang:alpine → alpine runtime)
├── docker-compose.yml       # ceres service + ceres-sandbox container
├── config/                  # runtime configuration (TOML, auto-created if missing)
│   ├── appconfig.toml       # active platforms, TUI options
│   ├── endpoints.toml       # LLM endpoints (name, base_url, api_key)
│   ├── platformconfig.toml  # discord token/guild/user, timeouts
│   └── toolconfig.toml      # sandbox, web tools, wakeup/subagent/timezone settings
├── agents/                  # agent definitions (one .toml per agent, auto-created)
├── wakeups/                 # persisted wakeups, one .toml per agent
├── memories/                # persistent memory, one folder per agent
├── logs/                    # log.txt (always appended; TUI mode logs file-only)
├── internal/
│   ├── agent/               # Agent (task queue + worker), agent TOML loading, subagent wiring
│   ├── app/                 # app lifecycle: load configs, registries, start agents, cron
│   ├── bubbletea/           # TUI (agent list, viewport, textarea, token rendering)
│   ├── commands/            # slash commands: help, clear, compress, interrupt, wake
│   ├── constants/           # file/directory path constants
│   ├── history/             # History/Entry/Token types (chat + streaming tokens)
│   ├── inference/           # LLM client: streaming, tool-call loop, compression, endpoints
│   ├── platforms/           # built-in Discord platform (gateway listener for DMs)
│   ├── tools/               # built-in tools (bash, execute_code, files, web, discord, ...)
│   └── wakeup/              # WakeUp scheduling (cron + one-shot) and TOML persistence
└── pkg/                     # small public-ish packages
    ├── command/             # slash command registry + external registration hook
    ├── config/              # dynamic TOML config (dot-notation reads, auto-write defaults)
    ├── handles/             # interfaces: AgentHandle, WakeupHandle, Task, TaskResult
    ├── platform/            # platform registry + external registration hook
    └── tool/                # tool registry + external registration hook
```

## Getting started

### Prerequisites

- Go ≥ 1.26 (or just use Docker)
- A Docker daemon (for the sandbox; disable it with `sandbox.active = false` in `toolconfig.toml`)
- An OpenAI-compatible LLM endpoint that speaks the **Responses API** (`/v1/responses`) — recent llama.cpp and Ollama builds both work

### Run with Docker (recommended)

```bash
docker compose up --build
```

This starts:

| Service  | Image              | Purpose                                            |
|----------|--------------------|----------------------------------------------------|
| `ceres`  | built from `Dockerfile` | the agent platform itself                    |
| `sandbox`| `python:3.11-slim` | isolated container for the bash/python/file tools  |

The compose file mounts the repo into the sandbox (`/workspace`) and the Docker socket into Ceres so it can exec into the sandbox.

### Run from source

```bash
go run .
```

On first start, Ceres creates missing `config/`, `agents/` (a default `Ceres` agent), `wakeups/`, and `logs/` automatically.

### Console

While running, the console accepts three commands (one per line):

| Command  | Effect                                                        |
|----------|---------------------------------------------------------------|
| `tui`    | enter the full-screen TUI (Ctrl+C inside the TUI leaves it)   |
| `reload` | shut down and restart the app (re-reads all configs)          |
| `exit`   | graceful shutdown                                             |

### TUI

- Left panel: agent list (Tab to focus, arrows to move, Enter to select).
- Right panel: chat viewport with markdown rendering (Dracula theme), an info bar (agent, token usage vs. compression threshold, state), and a multi-line input (Enter to send, Shift+Enter/Down for newline).
- Reasoning tokens are shown in italics when `tui.show_reasoning = true`.
- Slash commands work in the input field.

## Configuration

All configs are TOML files under `config/`. `pkg/config` reads keys with dot notation and **writes any missing key's default value into the file on first access**, so the files grow into a complete reference over time.

### `config/appconfig.toml`

```toml
active_platforms = ["discord"]   # platforms started at boot (registered: discord)
cronjob_timezone = "CET"         # (currently informational; cron uses toolconfig timezone)

[tui]
  message_timeout = "120m"       # per-message task timeout in the TUI
  show_reasoning = true          # render reasoning tokens in the TUI
```

### `config/endpoints.toml`

```toml
[[endpoints]]
  name = "llama"                 # referenced by agents via endpoint = "llama"
  base_url = "http://host:11434/v1"
  api_key = "none"
```

### `config/toolconfig.toml` (selected keys)

| Key | Default | Purpose |
|-----|---------|---------|
| `timezone` | `"Local"` | IANA zone for `get_time` and cron schedules |
| `sandbox.active` | `false` | enable the Docker sandbox tools |
| `sandbox.container_name` | `"ceres-sandbox"` | container to exec into |
| `sandbox.timeout` | `120s` | default per-command timeout |
| `sandbox.docker_host` | `""` | optional custom Docker host |
| `searxng.url` | `http://localhost:8080` | SearXNG instance (`format=json` must be enabled) |
| `firecrawl.url` / `firecrawl.api_key` | `http://localhost:3002` | Firecrawl scrape service |
| `execute_code.timeout` / `max_output_b` | `180s` / `20480` | Python snippet limits |
| `file_read.max_size_b` | `40960` | max bytes for `file_read` |
| `view_image.max_size_kb` | `4096` | max image size for `view_image` |
| `discord.max_file_size_kb` | `8192` | per-attachment limit for the discord tool |
| `memory.write_max_bytes` | `200000` | max size for memory write/insert/str_replace |
| `subagent.timeout` | `1h` | timeout for every subagent call |
| `wakeup.timeout` | `3h` | fixed timeout for agent-created wakeups |
| `wakeup.can_create_repeatable` | `false` | allow agents to create cron-based wakeups |

### `config/platformconfig.toml`

```toml
[discord]
  bot_token = "..."
  guild_id  = "..."
  user_id   = "..."             # only this user may DM the bot
  agent_name = "Ceres"          # agent the discord platform talks to
  message_timeout = "60m"
```

The Discord platform only reacts to **DMs** from the configured user (needs the DM intent + message content intent on the bot). Image attachments are downloaded and passed to the model as base64 images.

## Agents

Each `agents/<name>.toml` defines one agent (or several via `quantity`):

```toml
endpoint = "llama"              # name from endpoints.toml
model_name = "qwen3.8"
name = "Ceres"
description = "The main agent of the system."
reasoning_effort = "medium"     # none | low | medium | high
reasoning_summary = false       # use reasoning summaries instead of raw reasoning
quantity = 1                    # >1 spawns "name-1".."name-n"
subagent = false                # true = callable via the subagent tool
max_tool_iterations = 1000      # tool-call loop cap per task
compression_threshold = 180000  # tokens at which history gets compressed
num_messages_to_keep = 10       # history items kept verbatim after compression
compressions_promt = "..."      # prompt used to summarize old history

tools = ["bash", "execute_code", "..."]

system_prompt = "You are <name> a helpful AI assistant."   # <name> is substituted
```

Agents process **one task at a time**; further messages queue up. The currently configured agents:

- **Ceres** — main agent (all tools incl. `subagent`, `discord`, `wakeup_*`).
- **Researcher** — subagent restricted to `web_search`, `web_extract`, `get_time`.

## Tools (built-in)

| Tool | What it does |
|------|--------------|
| `bash` | run a shell command in the sandbox (timeout capped by `sandbox.timeout`) |
| `execute_code` | run a Python 3 snippet in the sandbox (base64-piped, output truncated) |
| `file_read` | read a sandbox file with line numbers (size-capped, line slicing) |
| `file_edit` | `write` / `insert` / `str_replace` on sandbox files |
| `web_search` | query a SearXNG instance (title/URL/snippet) |
| `web_extract` | scrape a URL to Markdown via Firecrawl |
| `view_image` | pull an image from the sandbox into the chat as a base64 image |
| `get_time` | current date/time in a given or default IANA timezone |
| `discord` | create/post/list/remove guild text channels, DM the user, attach sandbox files |
| `memory_read` / `memory_edit` | list/read/create/insert/str_replace/delete inside `memories/<Agent>/` |
| `subagent` | `list` available subagents, or `call` one/more in parallel with `summary_instructions` |
| `wakeup_read` | list own wakeups / read a wakeup's prompt chain |
| `wakeup_edit` | add (one-shot or cron, if allowed) / remove wakeups (protected ones are read-only) |

External tools/platforms/commands can be added through the `RegisterExternal()` hooks in `pkg/tool/all.go`, `pkg/platform/all.go`, `pkg/command/all.go`.

## Slash commands

| Command | Description |
|---------|-------------|
| `/help` | list all commands |
| `/clr` | queue a clear of the agent's chat history |
| `/cmp` | queue a manual compression of the chat history |
| `/itr` | interrupt the agent's active stream (partial output is kept) |
| `/wake list` | show registered wakeups (schedule, protected flag) |
| `/wake run <name>` | execute a wakeup immediately (in a fresh context) |

## Wakeups

Persisted per agent in `wakeups/<AgentName>.toml`. Either `fire_at` (RFC3339, one-shot) or `cron_spec` (recurring) — never both. When a wakeup fires, the agent's history is cleared and the wakeup's `prompts` are sent one after another in the same fresh context, so later prompts can build on earlier ones. One-shot wakeups delete themselves after firing. Wakeups marked `protected = true` can be listed and read by the agent but not removed (the `wakeup_edit` tool refuses; `protected` is forced to `false` whenever the agent creates one).


## Logging

All `log`/`slog` output goes to `logs/log.txt` (append) and stderr. In TUI mode stderr is redirected into the file only, to keep the terminal clean.

## Current state

Ceres is an actively developed, working system: streaming chat over TUI + Discord, with a full tool set, subagents, wakeups, memory, compression, and reasoning. The codebase is ~7k lines of Go, no test suite yet.
A `trading` git branch (Alpaca trading experiments) exists alongside `master`.

