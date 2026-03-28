# Configuration

This project ships a safe starter config at [`config/lumen.example.yaml`](../config/lumen.example.yaml). Copy it to `config/lumen.yaml` and keep your real secrets there.

```bash
cp config/lumen.example.yaml config/lumen.yaml
```

`config/lumen.yaml` is git-ignored.

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

Controls runtime shape and context budgeting.

Important fields:

- `name`: agent name exposed to the runtime prompt
- `workspace_root`: filesystem root available to tools
- `session_dir`: where runtime state, logs, and session files live
- `memory_dir`: durable memory directory
- `max_agent_loops`: max model/tool loop iterations per turn
- `max_tool_calls_per_turn`: hard cap on tool calls from one model response
- `history_compaction.*`: session compaction settings

### `llm`

Controls provider and model behavior.

Important fields:

- `api_type`: `openai` or `codex`
- `base_url`: provider API root
- `api_key` or `api_key_env`
- `model`
- `reasoning_effort`
- `max_tokens`
- `context_window_tokens`
- `inject_message_timestamps`
- `timeout`

`inject_message_timestamps` is important in this runtime because timestamps are added to model-visible messages and tool outputs to help the agent stay grounded.

### `tools`

Controls which tools exist and how shell execution behaves.

Important fields:

- `enabled`
- `exec_shell`
- `exec_timeout`
- `max_file_bytes`
- `max_search_results`
- `max_command_output_bytes`
- `allowed_commands`

If `allowed_commands` is empty, shell execution is unrestricted by this config layer.

### `background_tasks`

Controls sub-agent behavior.

Important fields:

- `default_min_runtime`
- `inject_current_time`
- `max_event_log_entries`
- `sandbox.*`

This section is what makes long-running background tasks more reliable and more inspectable.

### `discord`

Controls Discord access and upload handling.

Important fields:

- `bot_token`
- `allow_direct_messages`
- `allowed_guild_ids`
- `allowed_dm_user_ids`
- `allowed_outbound_channel_ids`
- `guild_session_scope`
- `reply_to_message`
- `download_incoming_attachments`
- `incoming_attachments_dir`

If `download_incoming_attachments` is enabled, uploaded files are saved locally and their URLs are replaced with local paths in prompt text.

### `gifs`

Optional GIF search support.

### `heartbeat`

Controls scheduled proactive runs.

Important fields:

- `every`
- `model`
- `light_context`
- `isolated_session`
- `ack_max_chars`
- `show_ok`
- `show_alerts`
- `use_indicator`
- `event_poll_interval`
- `active_hours.*`
- `target.*`

### `event_webhook`

Optional HTTP endpoint that queues heartbeat events.

### `skills`

Controls where `SKILL.md` manuals are loaded from.

### `mcp`

Defines external MCP servers whose tools should be exposed at startup.

## Important knobs

### Context and continuity

- `llm.inject_message_timestamps`
- `app.history_compaction.enabled`
- `app.history_compaction.trigger_tokens`
- `app.history_compaction.target_tokens`
- `app.history_compaction.preserve_recent_messages`

### Background work

- `background_tasks.default_min_runtime`
- `background_tasks.inject_current_time`
- `background_tasks.max_event_log_entries`

### Sandboxing

- `background_tasks.sandbox.enabled`
- `background_tasks.sandbox.force`
- `background_tasks.sandbox.provider`
- `background_tasks.sandbox.release`
- `background_tasks.sandbox.architecture`
- `background_tasks.sandbox.machines_dir`
- `background_tasks.sandbox.setup_timeout`
- `background_tasks.sandbox.auto_cleanup`

### Discord uploads

- `discord.download_incoming_attachments`
- `discord.incoming_attachments_dir`

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
