# Ceres

**Ceres** is a Go-based framework for autonomous AI agents. It connects LLM endpoints (OpenAI-compatible *Responses* API) with tools, scheduled tasks (*wakeups*), platforms such as Discord, and an interactive TUI.

## Features

- **Multi-agent system** – a main agent plus subagents; tasks can be delegated to subagents in parallel (`subagent_call`), with cycle detection.
- **Tool calling** – the agent invokes tools via the OpenAI Responses API.
- **Per-agent sandbox** – each agent declares its own sandbox container (`sandbox_container_name` in its TOML), so several isolated sandboxes can run side by side; an empty string means the agent has no sandbox. Sandbox tools (bash, execute_code, file_*, view_image, discord attachments) exec into that container over the Docker API. Docker is optional: if the daemon is unreachable Ceres still starts (warning at boot) and the tools report exactly what's missing ("no sandbox container configured" / "not connected to docker daemon").
- **Scheduled wakeups** – one-shot (`fire_at`) or recurring (cron). Wakeups created by an agent are persisted in SQLite; external ones (from `wakeups.toml`) are read-only.
- **Persistent memory** – each agent has its own SQLite database (`memory_sql` tool).
- **Web tools** – web search (SearXNG) and content extraction (Firecrawl) returned as Markdown.
- **History compression** – when a token limit is exceeded, the chat is summarized automatically.
- **Persistent chat history** – each agent's conversation is stored in SQLite and restored on restart.
- **Platforms** – Discord (DM listener with typing indicator, image uploads, slash commands, per-DM agent selection via `/agent list` and `/agent choose`); Web (embedded dashboard UI with live history/token streaming via WebSocket, per-tab agent switching, agent sidebar with search and icons, dark/light mode, working-state indicator and token counter, Markdown + LaTeX rendering with marked/DOMPurify/highlight.js/KaTeX).
- **Reasoning/thinking** – support for reasoning summaries, optionally shown in the TUI.
- **TUI + CLI** – Bubble Tea TUI (agent list, chat, input) plus a CLI loop (`tui`, `reload`, `exit`).
- **Dynamic TOML config** – missing config values are written automatically with defaults, preserving existing comments and formatting (two-space indentation, multi-line strings, dotted keys).

## Project structure

```
main.go                 Entry point: logging, signal handling, CLI loop
internal/
  app/                  App lifecycle (start/shutdown), config & registry wiring
  agent/                Agent, task queue/worker, agent config (TOML)
  inference/            OpenAI client: streaming, tool calls, history compression
  history/              History data model + SQLite persistence (per agent)
  task/                 Task types (ask, clear, clear_ask, compress)
  wakeup/               Wakeup manager, scheduler, SQLite DB + TOML loader
  tools/                Tool implementations (bash, file_*, web_*, memory_sql, ...)
  platforms/discord/    Discord platform (gateway/DM, slash commands, agent selection)
  platforms/web/        Web platform (embedded UI, WebSocket streaming)
  bubbletea/            TUI (list, viewport, textarea, rendering)
  commands/             Slash commands (help, clear, compress, interrupt, wakeup)
  constants/            Paths
pkg/                    Registries & interfaces (tool, command, platform, config, handles)
config/                 TOML config (app, endpoints, wakeups)
agents/                 Agent definitions (TOML)
database/               SQLite DBs (memory per agent, histories, wakeups)
```

## Core concepts

- **Agent** – wraps an LLM client, a task queue, and a worker. Tasks are processed serially; new tasks land in the queue.
- **Task** – the unit of work: `ask`, `clear`, `clear_ask`, or `compress`. Tool and wakeup results also flow through this model.
- **Wakeup** – a scheduled task source (timer or cron) that sends a task to an agent.
- **Tool** – a registered function with a JSON schema; invoked by the LLM, executed with the calling agent's handle (so sandbox tools resolve that agent's container).
- **Platform** – a channel through which messages reach an agent (e.g. Discord) and responses go back.

## Configuration

All configs live in `config/` as TOML:

- `appconfig.toml` – active platforms, platform settings (Discord token/IDs, default agent), timezone, TUI options, global tool settings (Discord upload size)
- `endpoints.toml` – LLM endpoints (base URL, API key), infrastructure (sandbox Docker host), and web tool settings (SearXNG, Firecrawl)
- `wakeups.toml` – external/scheduled wakeups

Per-agent tool settings (timeouts, result limits, wakeup permissions) live in
each agent's file under `[tool_settings.<tool>]`.

When Ceres writes a missing value it inserts only that key: strings containing newlines become multi-line strings, arrays go one element per line, and nested keys use dotted notation under the deepest existing table. See [`CONFIGURATION.md`](CONFIGURATION.md) for the full reference.

Agents are defined in `agents/*.toml` (endpoint, model, enabled tools, system prompt, subagent flag, compression). Each agent sets `sandbox_container_name` to the sandbox container its sandbox tools connect to (`""` = no sandbox); with `quantity > 1` all instances share that one container.

## Running

Runs via Docker Compose (agent + sandbox container):

```bash
docker compose up --build
```

- `ceres` – the agent; `tty`/`stdin` open for the CLI loop
- `sandbox` – Python container the default agent's sandbox tools exec into (each agent's container is chosen by its `sandbox_container_name`; add more services here for more sandboxes)
- the Docker socket is mounted so Ceres can exec into the sandbox(es)
- Docker is optional – Ceres also runs without it; sandbox tools then just report the missing daemon

While running: `tui` (start the TUI), `reload` (reload config/agents), `exit` (stop).

## Tech stack

Go 1.27 · Bubble Tea v2 / Lip Gloss / Glamour (TUI) · discordgo · Docker SDK · openai-go v3 · robfig/cron · modernc.org/sqlite · pelletier/go-toml/v2

---

### Open items / roadmap

- Agentic *graph* system (`GraphTask`): multiple agents as nodes, prompts as edges
- External wakeups should be able to trigger graph tasks as well
- Task variables as input before start, to make graph tasks more flexible
