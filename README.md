# Element Orion

Element Orion is a Go-based Discord runtime for people who do not just want an agent that can *do things*, but an agent that can stay coherent once the work gets messy.

Plenty of projects already cover the obvious checklist: model calls, tools, Discord replies, files, web access, background jobs. That is not the interesting part anymore.

Element Orion exists for the part that usually breaks right after the demo:

- keeping foreground chat separate from worker execution
- making long-running work inspectable instead of magical
- giving the model real startup context instead of vague implied state
- handling uploads, wakeups, heartbeats, and session continuity like runtime concerns
- treating context pressure as an operational problem, not just a prompt-writing problem

## Why Element Orion exists

If OpenClaw already gives you “agent with tools,” Element Orion is the answer to a different question:

**What should the runtime look like if you want that agent to be stable, inspectable, and actually livable inside Discord?**

The core bet is that agent quality is shaped as much by runtime architecture as by prompting.

Element Orion has a few strong opinions:

- the user should talk to one visible agent, not to worker boilerplate
- workers should run in their own lane and hand results back cleanly
- prompts should be built from actual runtime state and durable files
- history should be managed deliberately instead of growing until it rots
- proactive behavior should be scheduled and inspectable, not spooky

This means Element Orion is less about “here are twenty features” and more about **how those pieces are wired together so the system keeps its shape over time**.

## What feels different in practice

The best way to think about Element Orion is not as a pile of abilities, but as a runtime that enforces boundaries:

- foreground chat stays user-facing
- background workers stay operational
- the dom agent owns the final user reply
- logs exist for the work that happened
- session continuity lives in files and controlled history, not wishful memory

That is the part that makes it useful as a companion-style runtime instead of just another tool-calling wrapper.

## Runtime model

Element Orion has four main loops:

1. foreground Discord conversation loop
2. background worker loop
3. heartbeat loop
4. optional cron-style wakeup loop

Those loops share config, tools, and runtime services, but they do not all behave the same way.

### Foreground conversation loop

The dom agent lives here.

For a normal chat turn, Element Orion:

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
- when the worker finishes or fails, Element Orion creates an internal handoff event for the dom agent
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

Element Orion also supports one-shot wakeups that are more precise than the heartbeat interval.

These are useful for:

- tomorrow morning check-ins
- exact reminders
- “wake me up around 3 PM and follow up on this”

The key file is:

- [cron.go](internal/discordbot/cron.go)

## Prompt model

The system prompt is a major part of Element Orion’s behavior.

At startup, Element Orion injects:

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

Element Orion treats context as a limited runtime budget.

There are three different things to keep straight:

1. stored session history
2. loaded startup prompt material
3. actual model input after trimming

Those are not the same number.

Element Orion can:

- compact stored session history for continuity
- trim history against the input budget for live model calls
- let the model trigger compaction itself with `compact_context`
- show an estimated real input footprint in `/status`

The key files are:

- [agent.go](internal/agent/agent.go)
- [compaction.go](internal/agent/compaction.go)

## Slash commands

Element Orion currently ships a few core Discord slash commands:

- `/new` starts a fresh session
- `/stop` cancels the active session
- `/status` shows session, worker, and context state
- `/compact` compacts the current session history

These are public in-channel replies, not “only you can see this” interaction messages.

## Setup

This is the shortest path from clone to a working bot.

### 1. What you need first

Before you start, make sure you have:

- Go installed
- a Discord bot token, or a Discord user token if you want user-mode auth
- an OpenAI-compatible API key
- a machine where the bot can keep local state on disk

If you want Debian sandboxing later, you will also need:

- Linux with `systemd`
- `debootstrap`
- `systemd-nspawn`
- `machinectl`
- optional passwordless `sudo` if you want Element Orion to manage sandboxes without running fully as root

### 2. Clone the repo

```bash
git clone https://github.com/eli32-vlc/element-orion.git
cd element-orion
```

### 3. Create your real config

Start from the example:

```bash
cp config/lumen.example.yaml config/lumen.yaml
```

Your real config stays in `config/lumen.yaml`.
That file is git-ignored on purpose.

### 4. Fill in the minimum required config

At minimum, set:

- `discord.token_mode`
- `discord.bot_token` or `discord.user_token`
- `llm.model`
- `llm.api_key` or `llm.api_key_env`

You also need to decide where the bot is allowed to operate:

- `discord.allow_direct_messages`
- `discord.allow_group_direct_messages`
- `discord.allowed_guild_ids`
- `discord.allowed_dm_user_ids`
- `discord.allowed_outbound_channel_ids`

If you want one shared presence per Discord channel, keep:

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
  vision_enabled: false
  reasoning_effort: medium
  max_thinking_token: off
  temperature: 0.4
  max_tokens: 3200
  context_window_tokens: 28000
  inject_message_timestamps: true
  timeout: 180s
  kimi-no-think: false
  glm-no-think: false
```

What matters here:

- `model` decides the base brain
- `vision_enabled` controls whether image attachments are also forwarded as multimodal input
- `reasoning_effort: off` omits reasoning fields, while `none` is sent literally for providers that understand it
- `max_thinking_token` sets a provider thinking budget, or `off` to omit it
- `max_tokens` is reply budget
- `context_window_tokens` is the total assumed window
- `inject_message_timestamps` gives the model time grounding
- `timeout` controls how long a request can hang before failing
- `kimi-no-think` disables Kimi thinking by adding `chat_template_kwargs.thinking: false` to the OpenAI-compatible request body
- `glm-no-think` disables GLM thinking by adding `thinking.type: disabled` and `clear_thinking: true` to the OpenAI-compatible request body

### 7. Pick your starter tools

The example config already includes a broad tool set.

If you want a practical starter, keep at least:

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
    - list_scheduled_wakeups
    - cancel_scheduled_wakeup
    - add_rss_feed
    - list_rss_feeds
    - read_rss_feed
    - remove_rss_feed
    - search_web
    - search_news
```

Why these matter:

- file tools let the agent inspect and edit the workspace
- `exec_command` gives shell access
- `compact_context` lets the model clean up its own working set
- background task tools let it work without clogging chat
- scheduled wakeups give you precise follow-ups, including recurring cron wakeups managed by the running app
- RSS tools let it keep a saved feed list and pull recent posts on demand
- web and news make live research possible

### 8. Set up uploads properly

If you want Discord uploads to become usable local files, keep:

```yaml
discord:
  download_incoming_attachments: true
  incoming_attachments_dir: ./.element-orion/incoming-attachments
```

That means when a user uploads a file:

1. Element Orion downloads it
2. stores it locally
3. rewrites the message context to include the local path

This is one of the details that makes real file work much less annoying.

### 9. Set up heartbeat if you want proactive behavior

Heartbeat is optional, but it is what makes Element Orion feel more like a caretaker instead of a purely reactive bot.

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

Element Orion works best when compaction is on.

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
    machines_dir: ./.element-orion/sandboxes
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
go run ./cmd/element-orion serve -config config/lumen.yaml
```

If you prefer a built binary:

```bash
go build -o element-orion ./cmd/element-orion
./element-orion serve -config config/lumen.yaml
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
  name: Element Orion
  workspace_root: .
  session_dir: ./.element-orion
  memory_dir: ./.element-orion/memory
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
  vision_enabled: false
  reasoning_effort: medium
  max_thinking_token: off
  temperature: 0.4
  max_tokens: 3200
  context_window_tokens: 28000
  inject_message_timestamps: true

discord:
  token_mode: bot
  bot_token: your-discord-token
  user_token: ""
  allow_direct_messages: true
  allow_group_direct_messages: false
  guild_session_scope: channel
  download_incoming_attachments: true
  incoming_attachments_dir: ./.element-orion/incoming-attachments

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
    - list_scheduled_wakeups
    - cancel_scheduled_wakeup
    - add_rss_feed
    - list_rss_feeds
    - read_rss_feed
    - remove_rss_feed
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

dream_mode:
  enabled: false
  every: 6h
  model: ""
  light_context: false
  use_indicator: false
  sleep_hours:
    timezone: Australia/Brisbane
    start: "23:00"
    end: "06:00"
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
cmd/element-orion/            CLI entrypoint and utility subcommands
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
- `.element-orion/` is runtime state
- identity and continuity files like `BOOTSTRAP.md`, `IDENTITY.md`, `USER.md`, `SOUL.md`, `HEARTBEAT.md`, `TASKS.md`, and `CODEBASE.md` are ignored by default

## Positioning

OpenClaw is part of the inspiration, but Element Orion is aiming at a different balance:

- more Discord-native companion behavior
- stronger worker visibility
- simpler operator control
- explicit runtime metadata
- foreground dom agent plus separate worker lane

In short: less agent theater, more observable runtime behavior.

## Skill Compatibility

Element Orion's native skill format is OpenClaw-style `skills/<name>/SKILL.md`, but the loader also understands Claude Code-compatible locations:

- project skills in `.claude/skills/**/SKILL.md`
- project commands in `.claude/commands/**/*.md`
- user skills in `~/.claude/skills/**/SKILL.md`
- user commands in `~/.claude/commands/**/*.md`

Both ecosystems use Markdown plus YAML frontmatter, so the compatibility layer is mostly about discovering Claude-style paths while preserving existing OpenClaw precedence and requirement filtering.
