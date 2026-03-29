# Lumen Agent

Lumen Agent is a Go-based Discord agent runtime for people who want a companion-style agent that is also inspectable, operational, and debuggable.

It is built around a simple idea: the hard part is not calling a model. The hard part is keeping behavior sane once you add sessions, files, long-running work, uploads, heartbeats, and background agents.

Lumen focuses on those runtime problems:

- long-running background workers
- inspectable background logs and tool output
- prompts that know the actual runtime state at startup
- uploaded Discord files becoming local paths automatically
- sessions that compact instead of growing forever
- optional Debian `systemd-nspawn` sandboxing for background work
- scheduled heartbeats and one-shot wakeups

## What Lumen is

Lumen is not just “LLM + tools + Discord.”

It is a runtime with a few strong opinions:

- the agent should know what environment it is actually running in
- long-running work should be inspectable
- the foreground chat and the worker lane should be separate
- users should talk to the dom agent, not to worker boilerplate
- durable files should carry identity and continuity better than vague chat memory
- context pressure is a runtime problem, not only a prompting problem

## Core capabilities

- Discord bot runtime with shared-channel or per-user session scope
- OpenAI-compatible providers, including Codex-style `/responses`
- Workspace tools for files, shell, search, Discord, weather, GIFs, web, and news
- Background workers with inherited snapshot context, minimum runtime budgets, event logs, cancellation, and status inspection
- Internal worker handoff: workers finish in the background and the dom agent is the one that speaks back to the user
- Context compaction both for stored session history and on-demand model-triggered compaction
- Heartbeat runs, queued system events, and precise one-shot wakeups
- Sandbox lifecycle management tools for Debian `nspawn`
- Skills system using `SKILL.md`
- MCP server integration

## Runtime model

Lumen has four main loops:

1. foreground Discord conversation loop
2. background worker loop
3. heartbeat loop
4. optional cron-style wakeup loop

Those loops share config, tools, and runtime services, but they do not all behave the same way.

### Foreground conversation loop

The dom agent lives here.

For a normal chat turn, Lumen:

1. resolves the channel session
2. builds the system prompt from runtime metadata and workspace files
3. trims history to fit the configured input budget
4. calls the model
5. executes tool calls
6. persists the compacted session history
7. sends the final reply back to Discord

The key files are:

- [service.go](internal/discordbot/service.go)
- [agent.go](internal/agent/agent.go)
- [prompt_context.go](internal/agent/prompt_context.go)

### Background worker loop

When the dom agent starts a background task, the worker gets a snapshot of the current chat context and then runs separately.

Important behavior:

- the worker inherits a copy of the chat at spawn time
- after that, the worker keeps its own private history
- the worker does not keep sharing live context with the main chat
- the worker does not directly talk to the user anymore
- when the worker finishes or fails, Lumen creates an internal handoff event for the dom agent
- the dom agent then replies to the user in normal language

This matters because it keeps sub-agents in the worker lane and keeps user-facing messaging owned by the dom agent.

The key file is:

- [background.go](internal/discordbot/background.go)

### Heartbeat loop

Heartbeat is a proactive maintenance loop. It can:

- read `HEARTBEAT.md`
- process queued system events
- run on a schedule
- use a different model
- use isolated or shared session context

Heartbeat is meant to behave more like a caretaker than a chat turn.

The key file is:

- [heartbeat.go](internal/discordbot/heartbeat.go)

### Precise wakeup loop

Lumen also supports one-shot wakeups that are more precise than the heartbeat interval.

These are useful for:

- tomorrow morning check-ins
- exact reminders
- “wake me up around 3 PM and follow up on this”

The key file is:

- [cron.go](internal/discordbot/cron.go)

## Prompt model

The system prompt is a major part of Lumen’s behavior.

At startup, Lumen injects:

- local time and UTC tracking time
- runtime metadata
- enabled tools
- loaded skills
- workspace files like `IDENTITY.md`, `USER.md`, `SOUL.md`, `CODEBASE.md`, `TASKS.md`
- heartbeat files
- memory shards

This gives the model a real picture of the current environment instead of pretending everything is implicit.

The key file is:

- [prompt_context.go](internal/agent/prompt_context.go)

## Context model

Lumen treats context as a limited runtime budget.

There are three different things to keep straight:

1. stored session history
2. loaded startup prompt material
3. actual model input after trimming

Those are not the same number.

Lumen can:

- compact stored session history for continuity
- trim history against the input budget for live model calls
- let the model trigger compaction itself with `compact_context`
- show an estimated real input footprint in `/status`

The key files are:

- [agent.go](internal/agent/agent.go)
- [compaction.go](internal/agent/compaction.go)

## Slash commands

Lumen currently ships a few core Discord slash commands:

- `/new` starts a fresh session
- `/stop` cancels the active session
- `/status` shows session, worker, and context state
- `/compact` compacts the current session history

These are public in-channel replies, not “only you can see this” interaction messages.

## Full setup guide

This section is the practical path from fresh clone to a working bot.

### 1. What you need first

Before you start, make sure you have:

- Go installed
- a Discord application and bot token
- an OpenAI-compatible API key
- a machine where the bot can keep local state on disk

If you want Debian sandboxing later, you will also need:

- Linux with `systemd`
- `debootstrap`
- `systemd-nspawn`
- `machinectl`
- optional passwordless `sudo` if you want Lumen to manage sandboxes without running fully as root

### 2. Clone the repo

```bash
git clone https://github.com/eli32-vlc/lumen-agent.git
cd lumen-agent
```

### 3. Create your real config

Start from the safe example:

```bash
cp config/lumen.example.yaml config/lumen.yaml
```

Your real config stays in `config/lumen.yaml`.
That file is git-ignored on purpose.

### 4. Fill in the minimum required config

At minimum, edit `config/lumen.yaml` and set:

- `discord.bot_token`
- `llm.model`
- `llm.api_key` or `llm.api_key_env`

You also need to decide how the bot is allowed to operate:

- `discord.allow_direct_messages`
- `discord.allowed_guild_ids`
- `discord.allowed_dm_user_ids`
- `discord.allowed_outbound_channel_ids`

If you want the bot to live like one shared presence in a Discord channel, keep:

```yaml
discord:
  guild_session_scope: channel
```

That is the most companion-like mode.

### 5. Choose how to provide your model key

You have two common choices.

#### Option A: put the key directly in config

```yaml
llm:
  api_key: your-key-here
```

This works, but environment variables are usually cleaner.

#### Option B: use an environment variable

Keep this in config:

```yaml
llm:
  api_key_env: OPENAI_API_KEY
```

Then export the real secret in your shell:

```bash
export OPENAI_API_KEY=your-key
```

### 6. Pick a sane starter model block

If you want a solid default, use something like:

```yaml
llm:
  api_type: codex
  base_url: https://api.openai.com/v1
  api_key_env: OPENAI_API_KEY
  model: gpt-5.4
  reasoning_effort: medium
  temperature: 0.4
  max_tokens: 3200
  context_window_tokens: 28000
  inject_message_timestamps: true
  timeout: 180s
```

What these matter for in plain words:

- `model` decides the base brain
- `max_tokens` is reply budget
- `context_window_tokens` is the total assumed window
- `inject_message_timestamps` gives the model time grounding
- `timeout` controls how long a request can hang before failing

### 7. Pick your starter tools

The example config already includes a broad tool set.

If you want a practical general-purpose starter, keep at least:

```yaml
tools:
  enabled:
    - send_discord_message
    - read_file
    - write_file
    - replace_in_file
    - list_dir
    - grep_search
    - exec_command
    - compact_context
    - start_background_task
    - list_background_tasks
    - get_background_task
    - get_background_task_logs
    - cancel_background_task
    - schedule_heartbeat_wakeup
    - search_web
    - search_news
```

What these do for you:

- file tools let the agent inspect and edit the workspace
- `exec_command` gives shell access
- `compact_context` lets the model clean up its own working set
- background task tools let it do long work without clogging chat
- wakeup scheduling gives you precise follow-ups
- web and news make live research possible

### 8. Set up uploads properly

If you want Discord file uploads to become usable local files, keep:

```yaml
discord:
  download_incoming_attachments: true
  incoming_attachments_dir: ./.lumen/incoming-attachments
```

That means when a user uploads a file:

1. Lumen downloads it
2. stores it locally
3. rewrites the message context to include the local path

This is a big quality-of-life feature for real file work.

### 9. Set up heartbeat if you want proactive behavior

Heartbeat is optional, but it is what makes Lumen feel more like a caretaker instead of a purely reactive bot.

A simple starter block looks like:

```yaml
heartbeat:
  every: 30m
  light_context: false
  isolated_session: true
  show_ok: false
  show_alerts: true
  use_indicator: true
  event_poll_interval: 5s
  active_hours:
    timezone: Australia/Brisbane
    start: "08:00"
    end: "23:00"
  target:
    guild_id: ""
    channel_id: your-channel-id
    user_id: your-user-id
```

Important behavior:

- `isolated_session: true` keeps heartbeat from polluting normal chat history
- `active_hours.timezone` should usually match the machine or human schedule you care about
- `target` decides where proactive messages go

### 10. Keep context under control

Lumen works best when compaction is on.

The example config already gives a decent starter:

```yaml
app:
  history_compaction:
    enabled: true
    trigger_tokens: 12000
    target_tokens: 7000
    preserve_recent_messages: 16
```

That means:

- older history gets summarized when it grows too large
- recent messages stay more exact
- the bot keeps continuity without hauling every old turn forever

### 11. Optional: enable Debian sandboxing for background workers

Only do this if your host can actually support it.

A starter block looks like:

```yaml
background_tasks:
  sandbox:
    enabled: true
    force: false
    use_sudo: true
    provider: nspawn
    release: stable
    architecture: amd64
    mirror: http://deb.debian.org/debian/
    machines_dir: ./.lumen/sandboxes
    setup_timeout: 20m
    auto_cleanup: true
```

This means:

- workers can request a Debian `nspawn` sandbox
- the dom agent can manage sandbox lifecycle if those tools are enabled
- shell execution for sandboxed background work runs in the container instead of directly on the host

If sandboxing is requested but not configured, background tasks will fail with a sandbox-manager error.

### 12. Run the service

Once config is ready, start the bot with:

```bash
go run ./cmd/lumen-agent serve -config config/lumen.yaml
```

If you prefer a built binary:

```bash
go build -o lumen-agent ./cmd/lumen-agent
./lumen-agent serve -config config/lumen.yaml
```

### 13. First-run checklist in Discord

Once the bot is online:

1. invite it to your server if needed
2. go to an allowed channel or DM
3. use `/new`
4. send a normal message
5. try `/status` to confirm context, session, and worker state look sane

If you want to test files:

1. upload a small text file
2. ask the bot what local path it downloaded to
3. ask it to inspect the file

If you want to test workers:

1. ask it to start a background task
2. wait a moment
3. use `/status`
4. ask what the worker is doing

### 14. A good starter config you can adapt

```yaml
app:
  name: Lumen Agent
  workspace_root: .
  session_dir: ./.lumen
  memory_dir: ./.lumen/memory
  load_all_memory_shards: false
  max_agent_loops: 32
  max_tool_calls_per_turn: 96
  history_compaction:
    enabled: true
    trigger_tokens: 12000
    target_tokens: 7000
    preserve_recent_messages: 16

llm:
  api_type: codex
  base_url: https://api.openai.com/v1
  api_key_env: OPENAI_API_KEY
  model: gpt-5.4
  reasoning_effort: medium
  temperature: 0.4
  max_tokens: 3200
  context_window_tokens: 28000
  inject_message_timestamps: true

discord:
  bot_token: your-discord-token
  allow_direct_messages: true
  guild_session_scope: channel
  download_incoming_attachments: true
  incoming_attachments_dir: ./.lumen/incoming-attachments

tools:
  enabled:
    - send_discord_message
    - read_file
    - write_file
    - replace_in_file
    - list_dir
    - grep_search
    - exec_command
    - compact_context
    - start_background_task
    - list_background_tasks
    - get_background_task
    - get_background_task_logs
    - cancel_background_task
    - schedule_heartbeat_wakeup
    - search_web
    - search_news

heartbeat:
  every: 30m
  isolated_session: true
  show_ok: false
  show_alerts: true
  use_indicator: true
  event_poll_interval: 5s
  active_hours:
    timezone: Australia/Brisbane
    start: "08:00"
    end: "23:00"
  target:
    guild_id: ""
    channel_id: your-channel-id
    user_id: your-user-id
```

### 15. Common setup mistakes

- forgetting to copy `config/lumen.example.yaml` to `config/lumen.yaml`
- setting `llm.context_window_tokens` unrealistically for the model/provider
- enabling heartbeat but not setting a valid target
- requesting sandboxing while sandbox support is disabled
- leaving attachment downloads off and then wondering why uploads are only links
- using the wrong `guild_session_scope` for the behavior you want
- exposing more tools than you actually want the agent to plan around

### 16. Where to go next

Once the bot is running, the next useful docs are:

- [Architecture](docs/architecture.md)
- [Configuration](docs/config.md)
- [Background Tasks](docs/background-tasks.md)
- [Sandboxing](docs/sandboxing.md)

## Docs

- [Architecture](docs/architecture.md)
- [Configuration](docs/config.md)
- [Background Tasks](docs/background-tasks.md)
- [Sandboxing](docs/sandboxing.md)

## Repo layout

```text
cmd/lumen-agent/            CLI entrypoint and utility subcommands
internal/agent/             model loop, prompt assembly, history trimming, compaction
internal/config/            YAML config loading, defaults, validation, path resolution
internal/discordbot/        Discord service, sessions, slash commands, uploads, heartbeat
internal/sandbox/           nspawn sandbox manager
internal/tools/             built-in tool registry and tool implementations
internal/llm/               provider adapters and serializers
internal/skills/            skill loading and prompt snapshots
config/lumen.example.yaml   safe starter config
docs/                       runtime, config, background, and sandbox docs
```

## Development

Run tests with:

```bash
go test ./...
```

Important local state:

- `config/lumen.yaml` is git-ignored
- `.lumen/` is runtime state
- identity and continuity files like `BOOTSTRAP.md`, `IDENTITY.md`, `USER.md`, `SOUL.md`, `HEARTBEAT.md`, `TASKS.md`, and `CODEBASE.md` are ignored by default

## Positioning

OpenClaw is part of the inspiration, but Lumen is aiming at a different balance:

- more Discord-native companion behavior
- stronger worker visibility
- simpler operator control
- explicit runtime metadata
- foreground dom agent plus separate worker lane

In short: less agent theater, more observable runtime behavior.
