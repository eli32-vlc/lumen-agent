# Lumen Agent

Lumen Agent is a Discord-native agent runtime for people who want something more operational than OpenClaw: stronger tool use, long-running sub-agents, better context continuity, downloadable uploads, and optional Debian `systemd-nspawn` sandboxing for background work.

It is built in Go, runs as a simple service, and is meant to be hacked on, self-hosted, and shipped.

## Why it exists

OpenClaw proved the shape. Lumen pushes the runtime harder.

- Better long-task handling with background sub-agents, minimum runtime budgets, cancellation, and inspectable task logs.
- Better grounding with timestamp injection on model-visible messages and tool outputs.
- Better continuity with automatic history compaction, durable workspace files, and session-aware context loading.
- Better file handling by downloading Discord uploads locally and replacing attachment links with real file paths.
- Better isolation with opt-in or forced Debian `nspawn` sandboxes plus sandbox lifecycle tools the main agent can control.
- Better operator visibility with per-task event logs and retrievable command/tool output.

## Release highlights

- Discord bot runtime with shared-channel or per-user session scopes
- OpenAI-compatible providers, including Codex-style `/responses`
- Workspace-scoped file, search, shell, Discord, and web tools
- Background task APIs with status lookup, log lookup, cancellation, and inherited runtime context
- Optional `systemd-nspawn` sandbox provisioning and container lifecycle management
- Skills loading with OpenClaw-style `SKILL.md` support
- MCP server integration
- Heartbeat runs, queued system events, and one-shot cron reminders
- Runtime metadata injection so the model wakes up knowing the host, model, config, directories, tools, compaction policy, and sandbox state

## Quick start

1. Copy the example config:

```bash
cp config/lumen.example.yaml config/lumen.yaml
```

2. Fill in your secrets in `config/lumen.yaml`.

3. Export your API key if you are using `llm.api_key_env`:

```bash
export OPENAI_API_KEY=your-key
```

4. Run the service:

```bash
go run ./cmd/lumen-agent serve
```

The runtime default still looks for `config/lumen.yaml`, and that file is ignored by git so your personal config stays local.

## Core ideas

### 1. The agent should know where it is

Each run now injects runtime metadata into the system prompt, including:

- active model
- provider type and base URL
- host, OS, arch, PID, and timezone
- workspace root, session dir, memory dir, and config path
- history compaction state
- timestamp injection state
- attachment download state
- background task and sandbox configuration
- enabled tools and MCP servers

This makes the model less likely to hallucinate its environment or forget how the runtime is configured.

### 2. Background agents should be inspectable

Sub-agents are not black boxes.

- `start_background_task` supports `min_runtime` and optional sandboxing
- `get_background_task` can include task events
- `get_background_task_logs` exposes command output and tool-by-tool progress
- background runs inherit current runtime history/context so they stop losing track of the parent task

If a user asks what a sub-agent is doing, the main agent now has the primitives to check instead of guessing.

### 3. Sandboxing should be operational, not decorative

When enabled, background-task shell execution can run inside a fresh Debian container managed through `systemd-nspawn`.

Exposed lifecycle tools:

- `list_sandbox_containers`
- `inspect_sandbox_container`
- `create_sandbox_container`
- `start_sandbox_container`
- `stop_sandbox_container`
- `delete_sandbox_container`

The implementation is aligned with the systemd interfaces around [`systemd-nspawn`](https://www.freedesktop.org/software/systemd/man/latest/systemd-nspawn.html), [`machinectl`](https://www.freedesktop.org/software/systemd/man/latest/machinectl.html), [`systemd-run`](https://www.freedesktop.org/software/systemd/man/latest/systemd-run.html), and the [`org.freedesktop.machine1`](https://www.freedesktop.org/software/systemd/man/org.freedesktop.machine1.html) D-Bus API.

## Repository layout

```text
cmd/lumen-agent/            CLI entrypoint
internal/agent/             agent loop, prompt assembly, compaction
internal/config/            YAML config loading and validation
internal/discordbot/        Discord service and attachment download flow
internal/sandbox/           nspawn sandbox manager
internal/tools/             tool registry and tool implementations
internal/llm/               provider clients and serializers
config/lumen.example.yaml   safe starter config checked into git
workspace/                  editable workspace content
skills/                     bundled or repo-local skills
```

## Configuration notes

Important knobs:

- `llm.inject_message_timestamps`: inject timestamps into every model-visible message and tool result
- `app.history_compaction.*`: compact long sessions into a synthetic summary before requests get too large
- `background_tasks.default_min_runtime`: keep sub-agents running long enough to do real work
- `background_tasks.sandbox.*`: opt-in or forced containerized background execution
- `discord.download_incoming_attachments`: download uploads locally and pass their saved paths into the model
- `mcp.servers`: expose external MCP tools

## Development

Run tests with:

```bash
go test ./...
```

The checked-in config is the example file only. Keep your real config in `config/lumen.yaml`.

## Positioning

If OpenClaw is the experiment, Lumen Agent is the operator-focused runtime: more inspectable, more controllable, and more ready for messy real sessions where uploads, long jobs, and runtime state actually matter.
