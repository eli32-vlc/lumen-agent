# Lumen Agent Bootstrap

This file is the startup orientation for an agent waking up inside the Lumen Agent repository.

Read this as a grounded map of the codebase, not as marketing copy. When you work here, you are operating inside the runtime that defines how Lumen itself thinks, prompts, stores memory, handles Discord sessions, manages workers, and exposes tools.

The most important idea to keep in mind is simple:

- this repo is not just "a Discord bot"
- this repo is a runtime
- the runtime has separate lanes for foreground chat, background workers, heartbeat maintenance, and scheduled wakeups
- prompt quality here depends on code paths, file loading, config resolution, and persisted state just as much as on model choice

If you are asked to change behavior, trace the full path through the relevant layer instead of patching a single file in isolation.

## What This Repository Is

Lumen Agent is a Go application that runs a Discord-native companion agent with:

- a foreground conversation loop
- a tool-execution loop
- background sub-agent tasks
- scheduled heartbeat runs
- precise app-managed wakeups
- optional sandboxed execution for background shell work
- optional dashboard and event-webhook HTTP surfaces
- prompt assembly from runtime metadata, workspace files, skills, memory, and saved session history

This project is opinionated in a few ways that matter operationally:

- the user should experience one coherent visible agent
- background workers should do work without becoming the user-facing speaker
- prompt context should come from actual files and runtime facts, not implied magic
- session history should be compacted deliberately instead of growing forever
- proactive behavior should be inspectable and schedulable, not spooky

## Read This Codebase In This Order

If you need to get productive quickly, these files give you the runtime skeleton fastest:

1. `cmd/lumen-agent/main.go`
   The CLI entrypoint. It loads config, prepares directories, resolves secrets, creates the tool registry, runner, Discord service, optional sandbox manager, and optional shared HTTP server.

2. `internal/config/config.go`
   The config contract for the whole app. It sets defaults, resolves paths relative to the config file, validates values, and derives important runtime helpers such as log paths, heartbeat enablement, input budget, compaction thresholds, and session/memory directories.

3. `internal/agent/prompt_context.go`
   The heart of prompt assembly. This file defines the base system prompt, the bootstrap ritual contract, runtime metadata injection, workspace-file loading, memory shard loading, heartbeat prompt rules, and the order in which durable files enter model context.

4. `internal/agent/agent.go`
   The model/tool loop. This file owns the turn loop, model requests, tool-call execution, automatic recovery/follow-through/wrap-up prompts, and the handoff between tool results and the next model step.

5. `internal/discordbot/service.go`
   The outer Discord runtime shell. It owns sessions, slash commands, message intake, uploads, queueing, typing indicators, persistence, and the final bridge back to Discord.

6. `internal/discordbot/background.go`
   The worker lane. It shows how background tasks inherit a snapshot of chat state, run independently, log events, optionally use sandboxes, and hand results back to the dom-agent lane instead of replying directly.

7. `internal/discordbot/heartbeat.go`
   The proactive maintenance lane. It handles heartbeat intervals, queued system events, checklist loading, delivery rules, and the "HEARTBEAT_OK" contract.

8. `internal/tools/registry.go`
   The tool surface. It registers built-in tools, enforces enabled-tool policy, resolves workspace paths, protects config files from accidental access, and wires optional MCP tool registration.

9. `docs/architecture.md`, `docs/config.md`, `docs/background-tasks.md`, and `docs/sandboxing.md`
   These are useful operator docs and generally align with the code, but source wins when there is ambiguity.

## High-Level Runtime Map

Think of the runtime as four loops sharing one config and one general worldview, but not one identical execution mode.

### 1. Foreground Discord conversation loop

Main ownership:

- `internal/discordbot/service.go`
- `internal/agent/agent.go`
- `internal/agent/prompt_context.go`

Rough flow:

1. a Discord message arrives
2. attachment references may be rewritten to downloaded local paths
3. the correct session is looked up or created
4. prompt context is assembled from runtime metadata, workspace files, skills, and memory
5. stored history is trimmed against the live input budget
6. the model is called
7. tool calls are executed
8. the loop continues until there is a final answer or a limit is reached
9. history is compacted for storage if needed
10. the reply goes back to Discord

### 2. Background worker loop

Main ownership:

- `internal/discordbot/background.go`
- `internal/tools/background_tasks.go`

Critical mental model:

- a worker starts from a snapshot of the current conversation
- after spawn, the worker has its own private history
- worker history does not continuously merge back into the foreground session
- when the worker finishes, Lumen creates an internal handoff so the dom agent can speak to the user naturally

This separation is deliberate and should be preserved when making changes.

### 3. Heartbeat loop

Main ownership:

- `internal/discordbot/heartbeat.go`
- `internal/heartbeatstate/state.go`

Heartbeat is not normal chat. It is a proactive loop with different constraints:

- it can poll queued system events
- it can inspect `HEARTBEAT.md`
- it can run in an isolated session
- it can use a distinct model
- it should either do useful work, stay quiet, or emit a short verified alert

### 4. Precise wakeup loop

Main ownership:

- `internal/discordbot/cron.go`
- `internal/tools/scheduled_wakeups.go`

This is how Lumen handles exact or recurring future follow-ups without pretending memory alone will handle them. Wakeups ultimately feed work back into the heartbeat path.

## The Prompt Model

Lumen does not rely on a tiny static system prompt. It builds a larger runtime-grounded prompt every turn.

The system prompt includes:

- the large base instruction block in `internal/agent/prompt_context.go`
- action-safety guidance
- output-efficiency guidance
- human-style guidance
- proactive guidance for heartbeat and background contexts
- local and UTC time
- runtime metadata
- visible session skills
- loaded workspace files
- optional memory files and memory shards

### Workspace file loading order

Normal prompt assembly loads these workspace files, in this order, when present:

1. `BOOTSTRAP.md`
2. `IDENTITY.md`
3. `USER.md`
4. `SOUL.md`
5. `CODEBASE.md`
6. `TASKS.md` or `tasks.md`
7. `HEARTBEAT.md` for heartbeat runs

Important detail:

- the loader explicitly checks `BOOTSTRAP.md` with this exact uppercase spelling
- if you want a file to participate in startup prompt context across platforms, use `BOOTSTRAP.md`, not `bootstrap.md`

### Why this matters

If you are creating durable operator context for this repo, put the right information in the right file:

- `BOOTSTRAP.md` for first-run orientation and identity ritual setup
- `IDENTITY.md` for the agent's stable identity
- `USER.md` for who the user is and how to address them
- `SOUL.md` for enduring values and preferences
- `CODEBASE.md` for a durable technical map of the repo
- `TASKS.md` for ongoing execution state
- `HEARTBEAT.md` for proactive checklist-driven maintenance

Do not treat these as interchangeable notes. The runtime makes semantic assumptions about them.

## What This Bootstrap File Should Do

Inside this repository, `BOOTSTRAP.md` should help a waking agent do three things well:

1. understand the architecture before editing
2. remember the repo's durable mental model
3. avoid common mistakes that come from treating Lumen like a simple bot wrapper

This file should not try to replace source-level inspection. It should reduce flailing and help the agent ask better questions of the code.

## CLI And Process Startup

The binary entrypoint is `cmd/lumen-agent/main.go`.

The default command is `serve`. There is also a `system-event` command for queueing heartbeat work.

`serve` does the following:

- parses `-config` and optional `-workspace-dir`
- loads and validates YAML config
- optionally overrides the workspace root with a flag or `LUMEN_WORKSPACE_DIR`
- creates the session directory
- creates the memory directory
- opens the audit logger under `<session_dir>/logs`
- resolves the LLM API key
- builds the tool registry
- builds the LLM client
- creates the agent runner
- optionally creates a sandbox manager
- creates the Discord service
- runs the Discord service plus optional dashboard/webhook HTTP listeners

Implications:

- broken startup is often a config, path-resolution, or secret-resolution issue
- the runtime depends heavily on derived paths from `config.Load`
- if a path seems wrong at runtime, inspect `internal/config/config.go` before assuming the caller supplied a bad string

## Configuration Hot Spots

The config file is the runtime contract. Small changes here materially alter behavior.

### Most important fields

#### `app.workspace_root`

- defines the root the filesystem tools operate inside
- defines where prompt-loaded workspace files are read from
- changing it changes what the agent can see as "the workspace"

#### `app.session_dir`

- stores runtime state
- session persistence, logs, heartbeat event files, reminders, uploads, and sandbox metadata depend on it

Derived locations include:

- `<session_dir>/logs`
- `<session_dir>/heartbeat-events`
- `<session_dir>/heartbeat-state.json`
- `<session_dir>/reminders.json`
- `<session_dir>/incoming-attachments` by default

#### `app.memory_dir`

- stores durable memory files and shards
- defaults to `<session_dir>/memory` if unset

#### `llm.context_window_tokens` and `llm.max_tokens`

These determine live input budget:

- effective input budget is roughly `context_window_tokens - max_tokens`
- bad settings here make the agent feel broken, forgetful, or timeout-prone

#### `discord.guild_session_scope`

This changes the social shape of the bot significantly:

- `channel` means a shared session per channel
- `user` means a separate session per user

If behavior seems socially wrong in guild channels, inspect this setting early.

#### `heartbeat.*`

Heartbeat only counts as enabled when:

- `heartbeat.every` is set
- `heartbeat.target.channel_id` is set
- `heartbeat.target.user_id` is set

That means "configured but not firing" often comes down to enablement rules, target values, or active-hours gating.

#### `background_tasks.sandbox.*`

These settings control whether background-task shell work can run inside Debian `nspawn` sandboxes. This matters for reproducibility, privilege boundaries, and Linux-only behavior.

#### `skills.load.*`

Skills come from multiple sources:

- bundled skills
- user skills
- configured extra directories
- workspace-local `skills/`

If skills seem missing, inspect path resolution and eligibility checks in `internal/skills/skills.go`.

## Tool Surface Map

The built-in tool registry is in `internal/tools/registry.go`. It wires up several groups of tools:

- filesystem tools
- shell execution
- Discord actions
- context compaction
- background-task control
- scheduled wakeups
- sandbox lifecycle control
- GIF search
- web and news lookup
- RSS feed management
- reminders
- MCP server tools

### Important operational details

#### Filesystem tools

- all file paths are resolved relative to `app.workspace_root` unless absolute
- paths escaping the workspace root are rejected
- the loaded config file is treated as protected because it may contain secrets
- direct access to the active config path is intentionally blocked

#### Shell execution

- `exec_command` runs through the configured shell
- if `tools.allowed_commands` is empty, the tool-level allowlist does not restrict commands
- command output is capped
- background tasks can route shell execution through a sandbox if sandbox context is active

#### Background tasks

- nested background tasks are explicitly blocked
- background tasks are visible through task-management tools
- logs include structured event histories

#### Scheduled wakeups

- these are app-managed, not external cron jobs
- a wakeup can be one-shot or cron-like recurring
- they ultimately feed heartbeat work

#### Reminders

- reminders are stored in JSON under the session directory
- they are runtime notes, not a general replacement for durable workspace memory

#### MCP

- MCP tools are registered dynamically from enabled server config
- startup and tool timeouts are configurable per server
- if startup fails during MCP registration, registry creation can fail

## Discord Service Behavior

The Discord service in `internal/discordbot/service.go` is the app shell around the model loop.

It owns:

- Discord connection lifecycle
- slash command syncing
- session lookup and creation
- queueing inbound prompts
- upload downloading and path rewriting
- slash command responses
- persistence of session history to disk
- coordination with heartbeat and background-task systems

### Slash commands

Core commands include:

- `/new`
- `/status`
- `/memory`
- `/compact`
- `/stop`

One very important detail from the code:

- when `/new` starts a fresh session and `BOOTSTRAP.md` exists in the workspace root, Lumen explicitly asks whether to run bootstrap now or skip it for the session

That means this file is not decorative. The service checks for it directly.

## Session And Persistence Model

Session persistence matters a lot in this codebase.

A session stores:

- IDs and routing key
- creation and update times
- in-memory history
- queue state
- cancel function
- skill snapshot
- persisted file path

Persisted session JSON lives under the configured session directory. If continuity, `/status`, or slash-command behavior seems wrong, inspect the session key rules and persistence path logic in the Discord service layer.

## History Compaction And Context Discipline

Lumen treats context pressure as an operational problem.

There are several different context numbers that should not be confused:

- stored session history size
- startup prompt payload size
- actual provider input size after trimming

The compaction path is mainly in:

- `internal/agent/compaction.go`
- `internal/agent/agent.go`
- `internal/config/config.go`

Important behaviors:

- stored history can be compacted for continuity
- recent messages can be preserved verbatim
- the model can also trigger explicit compaction through the `compact_context` tool

If the bot feels forgetful, do not assume a single prompt change is the right fix. Check:

- compaction thresholds
- preserve-recent count
- startup prompt size
- context window realism
- reply token budget
- timestamp injection overhead

## Background Task Mental Model

Background tasks are intentionally separate from foreground chat.

When a worker starts:

- it copies current session history
- it copies current skill snapshot
- it becomes its own event-logged task
- it can optionally run shell execution inside a sandbox

When it finishes:

- the worker result does not become a raw stream of user-facing updates
- the dom agent receives an internal handoff
- the dom agent owns the final reply

This design is easy to accidentally break if you "simplify" the flow by letting workers speak directly. Avoid doing that unless there is a very deliberate product decision behind it.

## Heartbeat And Proactive Behavior

Heartbeat is one of the most opinionated parts of the app.

The heartbeat loop:

- runs on an interval
- polls queued event files from `<session_dir>/heartbeat-events`
- optionally reads `HEARTBEAT.md`
- can run with light context
- can run in an isolated session

The heartbeat prompt tells the model to:

- treat checklist items as user-owned work
- act first on obvious low-risk items
- use local time as the user-facing clock
- avoid repeating already-delivered reminders
- mark completed heartbeat work in `HEARTBEAT.md`
- reply with `HEARTBEAT_OK` only when appropriate

If heartbeat seems noisy, inspect:

- `heartbeat.show_ok`
- `heartbeat.show_alerts`
- `heartbeat.use_indicator`
- active-hours configuration
- isolated-session settings
- the checklist contents of `HEARTBEAT.md`
- queued event files and heartbeat state

## Event Webhook And Shared HTTP Server

Lumen can optionally expose:

- an event webhook
- a dashboard

The webhook is implemented in `internal/eventwebhook/server.go`.

It:

- accepts POST requests
- authorizes via `X-Lumen-Webhook-Secret` or Bearer token when configured
- accepts text and mode
- enqueues heartbeat system events

If both webhook and dashboard are enabled on the same listen address, `internal/httpaux/server.go` can serve them on a shared listener.

## Dashboard

The dashboard in `internal/dashboard/server.go` is a lightweight runtime-inspection surface.

It serves:

- embedded UI assets
- `/api/state`

The state endpoint summarizes:

- recent model and tool activity
- active nodes and sessions
- memory state
- log-derived graph data

When dashboard state looks wrong, remember it is log-derived and config-derived. Check audit logs and state-building assumptions before changing the UI.

## Skills

Skills are not hardcoded in one list. They are discovered from multiple directories and filtered by eligibility.

Skill loading behavior lives in `internal/skills/skills.go`.

Key facts:

- eligible skills are discovered by locating `SKILL.md`
- user, bundled, extra-dir, and workspace skills all participate
- later sources override earlier ones by name
- frontmatter metadata can make a skill ineligible if env or binary requirements are missing

If a skill is expected but absent:

- check path resolution
- check requirements metadata
- check source precedence
- check whether the loader could stat the directory

## Local State And Files You Should Respect

Important runtime-managed or operator-managed paths:

- `config/lumen.yaml`
  Real runtime config. It is git-ignored and may contain secrets.

- `.lumen/`
  Runtime state in common local setups. This is ignored and usually includes sessions, logs, reminders, memory, downloads, heartbeat events, and sandboxes.

- `BOOTSTRAP.md`
  First-run agent bootstrap file loaded into prompt context.

- `IDENTITY.md`, `USER.md`, `SOUL.md`, `CODEBASE.md`, `TASKS.md`, `HEARTBEAT.md`
  Durable prompt-loaded files with different semantic roles.

### Gitignore note

This repository ignores local operator continuity files in `workspace/` and ignores runtime/config state such as:

- `.lumen/`
- `config/lumen.yaml`
- various local continuity files

At the time this bootstrap file was added, the repo did **not** ignore a root-level `BOOTSTRAP.md`, which is why this file can be tracked intentionally.

## How To Work Safely In This Repo

When asked to make changes here:

1. identify the affected runtime lane first
   Foreground chat, background task, heartbeat, wakeup scheduling, config loading, or tool execution.

2. inspect both behavior code and policy code
   For example, prompt behavior may come from both `internal/agent/prompt_context.go` and runtime code in `internal/discordbot/...`.

3. follow persisted-state consequences
   Many changes affect files on disk, not just live memory.

4. verify at the right level
   In this codebase that usually means `go test ./...`, targeted tests, config reasoning, and path or prompt inspection.

5. avoid claiming a flow is fixed unless the whole path now makes sense
   This repo has enough cross-layer behavior that partial understanding produces misleading fixes.

## Common Failure Modes

These are good first suspects when behavior looks wrong:

- config paths were resolved relative to the config file, not the caller's current directory
- the bot cannot resolve `OPENAI_API_KEY` or another secret env var
- `llm.context_window_tokens` is unrealistic for the chosen model
- `llm.max_tokens` is too large relative to the context window
- a path is blocked because it touches the protected config file
- heartbeat is "configured" but not actually enabled because the target is incomplete
- workers are expected to share live state with foreground chat, but they only inherit a spawn snapshot
- a sandbox was requested but the sandbox manager is disabled or unsupported on the host
- uploads are expected as files, but attachment downloading is off
- an expected skill is absent because requirement checks filtered it out
- the operator created `bootstrap.md` lowercase and expected the prompt loader to use it everywhere

## Practical Commands

The standard local commands are:

```bash
go run ./cmd/lumen-agent serve -config config/lumen.yaml
```

```bash
go build -o lumen-agent ./cmd/lumen-agent
./lumen-agent serve -config config/lumen.yaml
```

```bash
go test ./...
```

Queue a system event into heartbeat:

```bash
go run ./cmd/lumen-agent system-event -config config/lumen.yaml -text "Check urgent follow-ups" -mode now
```

## If You Need To Explain This Repo Quickly

Use this summary:

Lumen Agent is a Go-based Discord runtime for a companion-style agent. It builds prompts from real runtime metadata and durable files, runs a foreground dom-agent loop for user interaction, isolates long-running work in background workers, supports proactive heartbeat and scheduled wakeups, optionally manages sandboxed worker execution, and treats context pressure, continuity, and inspectability as first-class runtime concerns.

## Final Orientation

If you woke up here and need to act responsibly:

- start with the code, not assumptions
- remember that `BOOTSTRAP.md` is prompt-loaded and slash-command-visible
- treat prompt assembly, runtime metadata, config resolution, and persistence as equally important parts of behavior
- preserve the separation between dom-agent speech and worker execution
- keep proactive behavior intentional, inspectable, and low-noise
- prefer small, well-grounded fixes over vibes

This repository rewards careful tracing. When you understand the lane, the file ownership, and the persisted state involved, most changes become much easier to reason about.
