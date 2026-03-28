# Lumen Agent

Lumen Agent is a Go-based Discord agent runtime for people who want a more operational and inspectable alternative to OpenClaw.

It is built for the parts that usually break first in real usage:

- long-running sub-agents
- inspectable background logs and tool output
- uploaded files becoming local paths automatically
- sessions that need compaction instead of bloating forever
- optional sandboxed background execution
- prompts that know the actual runtime state at startup

## Why Lumen exists

Most agent wrappers are “model + tools + Discord.” Lumen is trying to be a runtime.

What it does better:

- background sub-agents with minimum runtime budgets
- status and log inspection for running tasks
- runtime metadata injection into the system prompt
- automatic Discord attachment download and path replacement
- opt-in or forced Debian `systemd-nspawn` sandboxing for background work
- durable context files and automatic history compaction

## Highlights

- Discord bot runtime with shared-channel or per-user session scope
- OpenAI-compatible providers, including Codex-style `/responses`
- Workspace tools for files, shell, search, Discord, weather, GIFs, web, and news
- Background tasks with event logs, log retrieval, cancellation, and inherited runtime context
- Sandbox lifecycle management tools for `nspawn` containers
- Skills system using OpenClaw-style `SKILL.md`
- MCP server integration
- Heartbeat runs and queued one-shot events

In other words: less vague agent theater, more control, more visibility, and a runtime that actually tells you what it has been up to.

## Quick start

1. Copy the example config:

```bash
cp config/lumen.example.yaml config/lumen.yaml
```

2. Fill in at least:

- `discord.bot_token`
- your allowed guild or DM settings
- `llm.model`
- `llm.api_key` or `llm.api_key_env`

3. Export your API key if you use `llm.api_key_env`:

```bash
export OPENAI_API_KEY=your-key
```

4. Run the service:

```bash
go run ./cmd/lumen-agent serve -config config/lumen.yaml
```

5. In Discord, use `/new` in an allowed channel, then talk normally in that channel.

## Docs

- [Configuration](docs/config.md)
- [Background Tasks](docs/background-tasks.md)
- [Sandboxing](docs/sandboxing.md)

## Repo layout

```text
cmd/lumen-agent/            CLI entrypoint
internal/agent/             agent loop, prompt assembly, compaction, runtime metadata
internal/config/            YAML config loading and validation
internal/discordbot/        Discord service, sessions, uploads, heartbeat
internal/sandbox/           nspawn sandbox manager
internal/tools/             built-in tool registry and implementations
internal/llm/               provider adapters and serializers
internal/skills/            skill loading and prompt snapshots
config/lumen.example.yaml   safe starter config
skills/                     bundled and workspace-local skills
workspace/                  editable workspace state
```

## Development

Run tests with:

```bash
go test ./...
```

`config/lumen.yaml` is git-ignored. Identity and user-state files like `BOOTSTRAP.md`, `IDENTITY.md`, `USER.md`, `SOUL.md`, `HEARTBEAT.md`, `TASKS.md`, and `CODEBASE.md` are also ignored by default.

## Positioning

OpenClaw is the inspiration. Lumen Agent is the more operator-focused runtime: better with long-running work, better with uploads and continuity, and better when you need to inspect what the agent is actually doing.
