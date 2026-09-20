# Ceres

**Ceres** is a Go-based framework for autonomous AI agents. It connects LLM endpoints (OpenAI-compatible *Responses* API) with tools, scheduled tasks (*wakeups*), platforms such as Discord, and an interactive TUI.

## Features

- **Multi-agent system** – a main agent plus subagents; tasks can be delegated to subagents in parallel (`subagent_call`), with cycle detection.
- **Tool calling** – the agent invokes tools via the OpenAI Responses API; sandbox tools run in isolation inside a Docker container.
- **Scheduled wakeups** – one-shot (`fire_at`) or recurring (cron). Wakeups created by an agent are persisted in SQLite; external ones (from `wakeups.toml`) are read-only.
- **Persistent memory** – each agent has its own SQLite database (`memory_sql` tool).
- **Web tools** – web search (SearXNG) and content extraction (Firecrawl) returned as Markdown.
- **History compression** – when a token limit is exceeded, the chat is summarized automatically.
- **Platforms** – Discord (DM listener with typing indicator, image uploads, slash commands).
- **Reasoning/thinking** – support for reasoning summaries, optionally shown in the TUI.
- **TUI + CLI** – Bubble Tea TUI (agent list, chat, input) plus a CLI loop (`tui`, `reload`, `exit`).
- **Dynamic TOML config** – missing config values are written automatically with defaults.

## Project structure

```
main.go                 Entry point: logging, signal handling, CLI loop
internal/
  app/                  App lifecycle (start/shutdown), config & registry wiring
  agent/                Agent, task queue/worker, agent config (TOML)
  inference/            OpenAI client: streaming, tool calls, history compression
  history/              Data model for history entries and tokens
  task/                 Task types (ask, clear, clear_ask, compress)
  wakeup/               Wakeup manager, scheduler, SQLite DB + TOML loader
  tools/                Tool implementations (bash, file_*, web_*, memory_sql, ...)
  platforms/            Discord platform (gateway/DM)
  bubbletea/            TUI (list, viewport, textarea, rendering)
  commands/             Slash commands (help, clear, compress, interrupt, wake)
  constants/            Paths
pkg/                    Registries & interfaces (tool, command, platform, config, handles)
config/                 TOML config (app, endpoints, platform, tool, wakeups)
agents/                 Agent definitions (TOML)
database/               SQLite DBs (memory per agent, wakeups)
```

## Core concepts

- **Agent** – wraps an LLM client, a task queue, and a worker. Tasks are processed serially; new tasks land in the queue.
- **Task** – the unit of work: `ask`, `clear`, `clear_ask`, or `compress`. Tool and wakeup results also flow through this model.
- **Wakeup** – a scheduled task source (timer or cron) that sends a task to an agent.
- **Tool** – a registered function with a JSON schema; invoked by the LLM, executed by the agent.
- **Platform** – a channel through which messages reach an agent (e.g. Discord) and responses go back.

## Configuration

All configs live in `config/` as TOML:

- `appconfig.toml` – active platforms, TUI options
- `endpoints.toml` – LLM endpoints (base URL, API key)
- `platformconfig.toml` – Discord (token, IDs)
- `toolconfig.toml` – sandbox, timeouts, web endpoints, wakeup options
- `wakeups.toml` – external/scheduled wakeups

Agents are defined in `agents/*.toml` (endpoint, model, tools, system prompt, subagent flag, compression).

## Running

Runs via Docker Compose (agent + sandbox container):

```bash
docker compose up --build
```

- `ceres` – the agent; `tty`/`stdin` open for the CLI loop
- `sandbox` – Python container where `bash`/`execute_code` run in isolation
- the Docker socket is mounted so Ceres can control the sandbox

While running: `tui` (start the TUI), `reload` (reload config/agents), `exit` (stop).

## Tech stack

Go 1.26 · Bubble Tea v2 / Lip Gloss / Glamour (TUI) · discordgo · Docker SDK · openai-go v3 · robfig/cron · modernc.org/sqlite · BurntSushi/toml

---

### Open items / roadmap

- Agentic *graph* system (`GraphTask`): multiple agents as nodes, prompts as edges
- External wakeups should be able to trigger graph tasks as well
- Task variables as input before start, to make graph tasks more flexible
