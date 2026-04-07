# Configuration

This project ships a safe starter config at [`config/lumen.example.yaml`](../config/lumen.example.yaml). Copy it to `config/lumen.yaml` and keep your real secrets there.

```bash
cp config/lumen.example.yaml config/lumen.yaml
```

`config/lumen.yaml` is git-ignored.

## Reading the config as a runtime map

Lumen’s config is not only “settings.” It is the runtime contract the agent is expected to live inside.

The config tells Lumen:

- how big the context budget is
- which tools exist
- where memory and session files live
- whether Discord is shared per channel or per user
- whether uploads become local files
- whether heartbeats and wakeups are active
- whether background workers can run in sandboxes
- whether MCP tools should appear at startup

That means bad config is often the root cause of weird agent behavior.

## Minimum setup

At minimum, configure:

- `discord.bot_token`
- your guild or DM allowlist
- `llm.model`
- `llm.api_key` or `llm.api_key_env`

Then run:

```bash
go run ./cmd/lumen-agent serve -config config/lumen.yaml
```

## Top-level sections

### `app`

Controls the overall runtime shape.

Key fields:

- `name`
  This becomes the runtime-facing agent name in metadata.

- `workspace_root`
  The root path tools operate inside unless a tool explicitly works elsewhere.

- `session_dir`
  Lumen’s runtime state directory. This is where session JSON, heartbeat events, logs, uploads, and other runtime state live.

- `memory_dir`
  Durable memory root for private shard files and curated memory.

- `load_all_memory_shards`
  If `false`, the prompt loads only the current and previous half-day shard.
  If `true`, it loads all shard markdown files in the memory directory.

- `max_agent_loops`
  Limits how many model/tool rounds one turn can take.

- `max_tool_calls_per_turn`
  Caps tool calls from a single model response. Useful for preventing runaway tool storms.

- `history_compaction`
  Controls how stored session history is summarized when it gets too large.

#### `app.history_compaction`

These settings control stored-history compaction, not the whole live prompt by themselves.

- `enabled`
  Turns storage compaction on or off.

- `trigger_tokens`
  When stored history grows past this estimate, Lumen compacts older history.

- `target_tokens`
  After compaction, Lumen tries to shrink the stored history to around this size.

- `preserve_recent_messages`
  Keeps the newest messages verbatim so the recent local thread stays crisp.

Practical advice:

- if the bot feels forgetful, do not immediately disable compaction
- instead, keep more recent messages and raise the trigger
- compaction is usually better than letting old noise flood the active prompt forever

### `llm`

Controls provider behavior and token budgeting.

Key fields:

- `api_type`
  `openai` or `codex`

- `base_url`
  Provider endpoint root

- `api_key` / `api_key_env`
  One of these needs to resolve successfully

- `model`
  Main model used for normal chat unless overridden

- `reasoning_effort`
  Passed through when the provider supports it

- `temperature`
  Normal completion temperature

- `max_tokens`
  Completion budget

- `context_window_tokens`
  Total assumed context window

- `inject_message_timestamps`
  If enabled, timestamps are written into model-visible messages. This helps with grounding, but it also increases prompt size slightly.

- `timeout`
- `request_max_attempts`
- `retry_initial_backoff`
- `retry_max_backoff`
- `headers`

#### How `llm` settings interact

The most important budgeting relationship is:

```text
input budget ~= context_window_tokens - max_tokens
```

That means if you set:

- `context_window_tokens: 1000000`
- `max_tokens: 64000`

then the live input budget is about 936000 tokens before other provider-side limits or safety margins.

Important behavior notes:

- `context_window_tokens` should reflect reality for the model you are actually using
- `max_tokens` is reply budget, so raising it lowers input budget
- `inject_message_timestamps` adds time grounding to model-visible messages, but it can also encourage the model to echo timestamps if the prompt is sloppy
- `reasoning_effort` only matters on providers that support it
- custom `headers` can be useful for provider gateways or compatibility layers

### `tools`

Controls the tool surface.

Key fields:

- `enabled`
  Explicit list of tool names to expose

- `exec_shell`
- `exec_timeout`
- `max_file_bytes`
- `max_search_results`
- `max_command_output_bytes`
- `allowed_commands`

Important note:

If `allowed_commands` is empty, shell execution is not restricted by this config layer. If you want a stricter execution surface, fill that list intentionally.

#### Tool policy advice

Think of `tools.enabled` as part of the prompt contract.

If a tool is listed there:

- it can be shown in startup metadata
- the model may plan around it
- docs and behavior expectations should match it

Useful patterns:

- small safe bot: only file tools, Discord tools, search, weather
- research-heavy bot: add web/news/background tools
- operator bot: add shell tools and sandbox lifecycle tools
- long-memory bot: keep `compact_context` enabled so the model can help manage its own working set

### `background_tasks`

Controls sub-agent behavior.

Key fields:

- `default_min_runtime`
  Default floor for worker runtime. If set, background workers keep going until they hit this floor or are genuinely blocked.

- `inject_current_time`
  Lets background workers get stronger time grounding.

- `max_event_log_entries`
  Keeps the in-memory event log bounded.

- `sandbox`
  Sandboxing policy and implementation settings.

#### `background_tasks.sandbox`

This block decides whether background-worker shell commands can be redirected into Debian `nspawn`.

- `enabled`
  Makes sandboxing available.

- `force`
  Makes sandboxing mandatory for every background task.

- `use_sudo`
  Lets Lumen run privileged sandbox commands through `sudo`.

- `provider`
  Current implementation is `nspawn`.

- `release`
  Debian release used for new rootfs creation.

- `architecture`
  Debian architecture, for example `amd64`.

- `mirror`
  Debian package mirror used for bootstrap.

- `machines_dir`
  Root directory where sandbox container files are stored.

- `setup_timeout`
  Maximum time allowed for rootfs/bootstrap setup.

- `auto_cleanup`
  If true, sandboxes can be deleted automatically after task cleanup instead of only being stopped.

Behavior summary:

- if a task asks for sandboxing and `enabled` is false, the task fails because no sandbox manager is configured
- if `force` is true, the dom agent does not need to ask for sandboxing per task
- if `use_sudo` is true, your host setup needs to support that operationally

### `discord`

Controls access rules and Discord-specific behavior.

Key fields:

- `bot_token`
- `allow_direct_messages`
- `allowed_guild_ids`
- `allowed_dm_user_ids`
- `allowed_outbound_channel_ids`
- `guild_session_scope`
- `reply_to_message`
- `download_incoming_attachments`
- `incoming_attachments_dir`

`guild_session_scope` matters a lot:

- `channel`
  One shared dom-agent session per guild channel

- other scopes
  Can produce more isolated user-specific behavior depending on how you configure it

If `download_incoming_attachments` is enabled, uploaded files are written to disk and prompt text is rewritten to local paths where possible.

#### Attachment flow

This is the fix for the "uploaded file is only a link" problem.

With download enabled:

1. user uploads a file
2. Lumen downloads it into `incoming_attachments_dir`
3. prompt content is rewritten to include the local path
4. file tools and shell tools can use that path directly

That makes uploads much more usable for real work.

### `gifs`

Optional GIF search support.

### `heartbeat`

Controls proactive runs.

Key fields:

- `every`
- `model`
- `light_context`
- `isolated_session`
- `ack_max_chars`
- `show_ok`
- `show_alerts`
- `use_indicator`
- `event_poll_interval`
- `active_hours`
- `target`

Important behavior:

- `isolated_session: true` means heartbeat runs do not reuse normal chat history
- `light_context: true` means heartbeat sees less loaded memory
- `target` tells Lumen where proactive replies are allowed to go

#### Heartbeat timezone behavior

There are two different time concerns here:

- machine-local time for human-facing behavior
- UTC or durable timestamps for internal tracking

For user-facing scheduling and active hours, the important field is:

- `heartbeat.active_hours.timezone`

If that is empty, Lumen falls back to local runtime behavior where appropriate.

This is especially important if you want morning check-ins and "tomorrow" logic to feel natural to the machine/user locale rather than reading like UTC everywhere.

#### `heartbeat.target`

Heartbeat needs a delivery target or it is not really enabled.

Typical setup:

- `guild_id`
- `channel_id`
- `user_id`

This tells Lumen where proactive messages belong.

### `event_webhook`

### `event_webhook`

Optional HTTP endpoint that turns external calls into heartbeat events.

This is useful for “tell Lumen when deploys finish,” “queue a follow-up after CI,” or “wake the heartbeat when another service notices something.”

### `skills`

Controls where `SKILL.md` manuals are loaded from.

### `mcp`

Defines external MCP servers and their startup/tool timeouts.

## What each section changes in practice

### If you want a companion-like shared channel bot

Prioritize:

- `discord.guild_session_scope: channel`
- heartbeat target set
- attachment downloads enabled
- moderate compaction enabled

### If you want a heavier operator bot

Prioritize:

- shell tools
- background task tools
- sandbox lifecycle tools
- larger context budget
- more careful tool caps

### If you want reliable long-running research work

Prioritize:

- `background_tasks.default_min_runtime`
- `background_tasks.max_event_log_entries`
- `compact_context`
- realistic `llm.context_window_tokens`

### If you want a quieter caretaker heartbeat

Prioritize:

- `heartbeat.isolated_session: true`
- `heartbeat.light_context: true`
- active hours
- target routing
- precise wakeups via `schedule_heartbeat_wakeup`
- scheduled wakeup inspection via `list_scheduled_wakeups`
- scheduled wakeup cancellation via `cancel_scheduled_wakeup`

## Important behavior combinations

### Big-context but still compacting

If you have a large context window, you may still want compaction on. Why:

- the full prompt includes system text, runtime metadata, files, and memory
- worker and heartbeat sessions still create noise
- compacted session history keeps continuity cleaner

### Shared guild channels

If you want the bot to behave like one presence in a server channel, set:

- `discord.guild_session_scope: channel`

This makes one shared session per channel instead of per human.

### Worker-heavy setups

If you rely on background workers, pay attention to:

- `background_tasks.default_min_runtime`
- `background_tasks.max_event_log_entries`
- `background_tasks.sandbox.*`
- heartbeat target settings if you want proactive wakeups and reminders

### Sandboxing with worker tools

If you enable sandbox tooling, also think about whether you want the agent to manage the container lifecycle itself.

That usually means enabling:

- `create_sandbox_container`
- `start_sandbox_container`
- `inspect_sandbox_container`
- `stop_sandbox_container`
- `delete_sandbox_container`

If you do not want the dom agent touching container lifecycle directly, keep those tools out of `tools.enabled` even if background sandboxing exists.

### Upload-heavy setups

If users send files often, enable:

- `discord.download_incoming_attachments: true`

and point:

- `discord.incoming_attachments_dir`

to a place inside `session_dir` or another path you actually want to manage.

## What `/status` reflects

`/status` uses config values plus live runtime state to estimate:

- effective input budget
- full prompt footprint
- in-window history size
- background worker context separation

So if the status output looks wrong, check:

- `llm.context_window_tokens`
- `llm.max_tokens`
- `app.history_compaction.*`
- loaded workspace files and memory size

## Example operating profiles

### Minimal chat companion

```yaml
app:
  max_agent_loops: 12
llm:
  context_window_tokens: 28000
  max_tokens: 3200
tools:
  enabled:
    - read_file
    - write_file
    - list_dir
    - grep_search
    - send_discord_message
discord:
  guild_session_scope: channel
heartbeat:
  every: 30m
```

Good for:

- casual Discord companionship
- low operational complexity
- smaller hosting footprint

### Research and worker-heavy setup

```yaml
app:
  max_agent_loops: 32
  history_compaction:
    enabled: true
    trigger_tokens: 20000
    target_tokens: 10000
    preserve_recent_messages: 24
llm:
  context_window_tokens: 1000000
  max_tokens: 64000
tools:
  enabled:
    - exec_command
    - search_web
    - search_news
    - start_background_task
    - list_background_tasks
    - get_background_task
    - get_background_task_logs
    - cancel_background_task
    - compact_context
background_tasks:
  default_min_runtime: 10m
```

Good for:

- codebase analysis
- long research tasks
- better inspection of worker progress

### Debian sandbox operator setup

```yaml
tools:
  enabled:
    - exec_command
    - start_background_task
    - get_background_task_logs
    - create_sandbox_container
    - inspect_sandbox_container
    - start_sandbox_container
    - stop_sandbox_container
    - delete_sandbox_container
background_tasks:
  sandbox:
    enabled: true
    provider: nspawn
    release: stable
    architecture: amd64
    use_sudo: true
```

Good for:

- Debian-specific debugging
- package install testing
- Linux environment reproduction

## Runtime precedence

Workspace root precedence is:

1. `-workspace-dir`
2. `LUMEN_WORKSPACE_DIR`
3. `app.workspace_root`

## Safe-to-commit rule

Commit:

- source code
- `config/lumen.example.yaml`
- docs

Do not commit:

- `config/lumen.yaml`
- `.lumen/`
- private identity or user-state files
