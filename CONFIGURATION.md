# Configuration

Ceres is configured with TOML files. Values that are missing are written into
the file with the default shown below the next time Ceres starts, so a config
file may start out empty or partial. Existing values, comments, and formatting
are left untouched.

- `config/appconfig.toml` – process-wide settings (platforms, timezone, TUI, global tool settings)
- `config/endpoints.toml` – the LLM endpoints agents can use, infrastructure settings, and web tool settings
- `agents/*.toml` – one file per agent (including per-agent tool settings)
- `config/wakeups.toml` – scheduled tasks that wake agents up

All paths are relative to the working directory Ceres is started in.

When a value is missing, Ceres inserts only that key and leaves the rest of the
file untouched. New keys are indented with two spaces, strings containing
newlines become multi-line strings (`"""..."""`), arrays are written one element
per line, and nested keys use dotted notation under the deepest existing table.
If no ancestor table exists, a new top-level header is created, e.g.:

```toml
[a]
  b.c.d = "value"
```

Hand-written header tables such as `[tool_settings.bash]` remain valid and are
not migrated. The edited file is validated before it is written atomically.

## App config

File: `config/appconfig.toml`

Process-wide settings. Everything here applies to all agents.

| Key                                      | Type            | Default | Description |
|------------------------------------------|-----------------|---------|-------------|
| `active_platforms`                       | array of string | `[]`    | Platforms to start (e.g. `["discord", "web"]`). |
| `timezone`                               | string          | `"Local"` | Timezone for the cron scheduler and the default timezone of `get_time`/`wakeup_add`. |
| `tui.message_timeout`                    | duration        | `"1h0m0s"` | How long the TUI keeps a conversation alive while idle. |
| `tui.show_reasoning`                     | bool            | `true`  | Show the model's reasoning output in the TUI. |
| `web.addr`                               | string          | `"127.0.0.1:8080"` | Address the web platform listens on. |
| `web.default_agent`                      | string          | `""`    | Initial agent for web clients; empty means the alphabetically first agent. Browsers can switch agents via the sidebar and remember the choice locally. |
| `web.show_reasoning`                     | bool            | `true`  | Send reasoning entries to the browser. |
| `web.message_timeout`                    | duration        | `"1h0m0s"` | Timeout for messages submitted from the browser. |
| `discord.default_agent`                  | string          | `"Ceres"` | Default agent that answers on Discord. Can be changed per DM with `/agent choose`. |
| `discord.bot_token`                      | string          | `"<token>"` | Discord bot token. |
| `discord.guild_id`                       | string          | `"<guild_id>"` | Discord guild for slash commands. |
| `discord.user_id`                        | string          | `"<id>"` | Discord user that receives DMs. |
| `discord.message_timeout`                | duration        | `"1h0m0s"` | Idle timeout for Discord conversations. |
| `tool_settings.discord.max_file_size_kb` | int             | `8192`  | Maximum file size the `discord_msg` tool may upload. |

Example:

```toml
active_platforms = []
timezone = "Local"

[tui]
  message_timeout = "1h0m0s"
  show_reasoning = true

[web]
  addr = "127.0.0.1:8080"
  default_agent = ""
  show_reasoning = true
  message_timeout = "1h0m0s"

[tool_settings]
  discord.max_file_size_kb = 8192
```

## Endpoints

File: `config/endpoints.toml`

An endpoint is an OpenAI-compatible *Responses* API server. Each entry is one
`[[endpoints]]` table:

| Key        | Type   | Required | Default | Description |
|------------|--------|----------|---------|-------------|
| `name`     | string | yes      | –       | Unique name used by agents (`endpoint` in an agent config). |
| `base_url` | string | yes      | –       | Base URL of the API, usually ending in `/v1`, e.g. `http://localhost:11434/v1`. |
| `api_key`  | string | no       | `""`    | API key sent as the bearer token. Use `"none"` for local servers that ignore it. |

Example:

```toml
[[endpoints]]
  name = "strata"
  base_url = "http://192.168.188.200:11434/v1"
  api_key = "none"

[[endpoints]]
  name = "openai"
  base_url = "https://api.openai.com/v1"
  api_key = "sk-..."
```

- An entry without a `name` or `base_url` is a startup error.
- Names must be unique; if a name is repeated, the last entry wins.
- If `endpoints.toml` or the `endpoints` key does not exist yet, a default
  `llama` endpoint (`http://localhost:11434/v1`, key `"none"`) is written.

### Infrastructure

The same file also holds process-wide infrastructure settings:

| Key                   | Type   | Default | Description |
|-----------------------|--------|---------|-------------|
| `sandbox.docker_host` | string | `""`    | Docker host for the sandbox tools (e.g. `unix:///var/run/docker.sock` or `tcp://host:2375`). Empty uses the Docker default. |

```toml
[sandbox]
  docker_host = ""
```

### Tool settings

The web tools read their settings from this file as well:

| Key                                   | Type   | Default | Description |
|---------------------------------------|--------|---------|-------------|
| `tool_settings.searxng.url`           | string | `"http://localhost:8080"` | SearXNG base URL used by `web_search`. |
| `tool_settings.firecrawl.url`         | string | `"http://localhost:3002"` | Firecrawl base URL used by `web_extract`. |
| `tool_settings.firecrawl.api_key`     | string | `""`    | Firecrawl API key. |

```toml
[tool_settings]
  searxng.url = "http://localhost:8080"
  firecrawl.url = "http://localhost:3002"
  firecrawl.api_key = ""
```

## Agents

Directory: `agents/`

Each `.toml` file defines one agent. The agent is named after the file: the
file `Ceres.toml` defines the agent `Ceres`. To run more agents, add more files.

| Key                     | Type            | Default | Description |
|-------------------------|-----------------|---------|-------------|
| `endpoint`              | string          | `"llama"` | Name of an endpoint from `endpoints.toml`. Must exist, otherwise startup fails. |
| `model_name`            | string          | `"qwen3.8"` | Model identifier at the endpoint (e.g. `"qwen3.8"`, `"gpt-4"`). |
| `description`           | string          | `"The main agent of the system."` | Human-readable description shown to other agents (e.g. by `subagent_list`). |
| `sandbox_container_name`| string          | `"ceres-sandbox"` | Docker container the sandbox tools connect to. An empty string means the agent has no sandbox. |
| `subagent`              | bool            | `false`  | Whether this agent is a subagent rather than a top-level agent. |
| `quantity`              | int             | `1`      | Number of instances to create. Values below 1 are treated as 1. |
| `reasoning_effort`      | string          | `"medium"` | Reasoning effort sent to the model: `"none"`, `"low"`, `"medium"`, or `"high"`. |
| `reasoning_summary`     | bool            | `false`  | Use the model's reasoning *summary* instead of its full reasoning output. |
| `system_prompt`         | string          | `"You are <name>, a helpful AI assistant."` | The agent's system prompt. `<name>` is replaced with the agent's name. |
| `enabled_tools`         | array of string | all registered tools | Tools the agent may call. An unknown tool name is a startup error. |
| `max_tool_iterations`   | int             | `100`    | Maximum tool-call rounds per request. Exceeding it aborts the request with an error. |
| `max_tool_result_chars` | int             | `100000` | Maximum size of a single tool result. Larger results are replaced by an error message. |
| `compression_threshold` | int             | `200000` | Token count that triggers chat-history compression. |
| `num_messages_to_keep`  | int             | `8`      | Number of recent messages kept after compression. |
| `compression_prompt`    | string          | see below | Prompt used when compressing the history. |
| `extra_body`            | string (JSON)   | `""`     | Optional JSON object with additional API parameters. Empty means none. |

The default `compression_prompt` is:

> This is a message injected by the system, your task is to compress the
> current chat. Dont ask any question, do the summary in this turn and keep
> everything that is relevant.

Example:

```toml
endpoint = "strata"
model_name = "qwen3.8"
reasoning_effort = "medium"
reasoning_summary = false
subagent = false
quantity = 1

description = "The main agent of the system."
sandbox_container_name = "ceres-sandbox"
system_prompt = "You are <name>, a helpful AI assistant."

enabled_tools = ["bash", "file_read", "web_search"]

max_tool_iterations = 100
max_tool_result_chars = 100000
compression_threshold = 200000
num_messages_to_keep = 8
```

Extra API parameters are given as a JSON object inside a TOML string:

```toml
extra_body = '{"temperature": 0.2}'
```

### Per-agent tool settings

Tools that support per-agent configuration read their values lazily from the
`tool_settings.<tool>` entries in the agent file, either as `[tool_settings.<tool>]`
headers or as dotted keys under `[tool_settings]`. Missing values fall back to
the defaults below; a default is only written into the file once the tool is
actually used. Agents without these settings stay unchanged at startup.

| Key                                          | Type     | Default | Description |
|----------------------------------------------|----------|---------|-------------|
| `tool_settings.bash.timeout`                 | duration | `"2m0s"` | Maximum runtime of a `bash` command. |
| `tool_settings.execute_code.timeout`         | duration | `"3m0s"` | Maximum runtime of an `execute_code` snippet. |
| `tool_settings.subagent.timeout`             | duration | `"1h0m0s"` | How long `subagent_call` waits for each task. |
| `tool_settings.memory_sql.max_rows`          | int      | `100`   | Maximum rows returned by `memory_sql`. |
| `tool_settings.memory_sql.max_columns`       | int      | `50`    | Maximum columns returned by `memory_sql`. |
| `tool_settings.view_image.max_size_kb`       | int      | `8192`  | Maximum image size accepted by `view_image`. |
| `tool_settings.wakeup.timeout`               | duration | `"1h0m0s"` | Maximum runtime of a wakeup task. |
| `tool_settings.wakeup.can_create_repeatable` | bool     | `false` | Whether the agent may create cron-based wakeups. |
| `tool_settings.wakeup.clear_history`         | bool     | `true`  | Whether a wakeup starts with an empty chat history. |

Example:

```toml
[tool_settings.bash]
  timeout = "2m0s"

[tool_settings.wakeup]
  timeout = "1h0m0s"
  can_create_repeatable = false
  clear_history = true
```

Notes:

- With `quantity = 3` and a file named `Ceres.toml`, the agents are named
  `Ceres-1`, `Ceres-2`, and `Ceres-3`. With `quantity = 1` the agent simply uses
  the file name.
- Every instance from one file shares the same endpoint, tools, sandbox
  container, and tool settings, but has its own chat history, memory database,
  and wakeups.
- If the `agents/` directory is empty, Ceres falls back to `agents/Ceres.toml`
  and creates it with all defaults.

## Wakeups

File: `config/wakeups.toml`

A wakeup schedules a task for an agent, either once at a fixed time or
repeatedly via a cron expression. Each entry is one `[[wakeups]]` table:

| Key           | Type            | Required | Default | Description |
|---------------|-----------------|----------|---------|-------------|
| `agent_name`  | string          | yes      | –       | Agent to wake up; must match an agent name (e.g. `Ceres` or `Ceres-1`). |
| `name`        | string          | yes      | –       | Unique wakeup name, shown by `wakeup_list` and used to inspect/remove it. |
| `description` | string          | no       | `""`    | Human-readable description. |
| `fire_at`     | datetime        | one of both | –    | One-shot time in ISO 8601 / TOML datetime format, e.g. `2026-09-25T10:00:00Z`. |
| `cron_spec`   | string          | one of both | –    | Cron expression for recurring wakeups, e.g. `"0 18 * * *"` for daily at 18:00. |
| `task_type`   | string          | yes      | –       | What to do: `"ask"`, `"clear"`, `"clear_ask"`, or `"compress"`. |
| `prompts`     | array of string | conditional | `[]` | Texts sent to the agent. Required (non-empty) for `ask` and `clear_ask`. |
| `timeout`     | string          | yes      | –       | Maximum task duration as a Go duration, e.g. `"30s"`, `"2m"`, `"1h"`. Must be positive. |

Task types:

- `ask` – send the prompts to the agent; the current chat history is kept.
- `clear` – clear the agent's chat history, without sending a prompt.
- `clear_ask` – clear the history first, then send the prompts.
- `compress` – compress the agent's chat history to free context space.

Example:

```toml
[[wakeups]]
  agent_name = "Ceres"
  name = "daily_summary"
  description = "Ask for a summary every evening."
  cron_spec = "0 18 * * *"
  task_type = "ask"
  prompts = ["Summarize what happened today."]
  timeout = "2m"

[[wakeups]]
  agent_name = "Ceres"
  name = "one_off_reminder"
  description = "Remind me once."
  fire_at = 2026-11-01T09:00:00Z
  task_type = "clear_ask"
  prompts = ["Start the day with a short plan."]
  timeout = "1m"
```

Rules and notes:

- Exactly one of `fire_at` and `cron_spec` must be set per entry.
- Expired one-shot wakeups (`fire_at` in the past) are silently ignored.
- Invalid entries (missing `agent_name`/`name`, invalid `timeout`, unknown
  `task_type`, empty `prompts` where required) are a startup error.
- Wakeups defined here are read-only for agents: they cannot be removed with the
  `wakeup_remove` tool. Only wakeups an agent created itself can be changed.
- If `wakeups.toml` or the `wakeups` key does not exist, an example entry is
  written. It uses `fire_at = 2024-01-01T12:00:00Z` and is therefore already
  expired, so it never runs.
