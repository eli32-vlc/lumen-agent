# Architecture

This document explains how Element Orion actually works as a runtime, not just as a list of features.

If you want the short version:

- Discord messages enter the dom-agent loop
- Element Orion assembles a large startup prompt from runtime metadata, workspace files, memory, skills, and session history
- the model can reply directly or call tools
- background work runs in a separate worker lane
- heartbeat and precise wakeups feed system events back into the same runtime
- context is continuously trimmed and compacted so the session stays usable

## The high-level shape

Element Orion has one runtime, but several different execution lanes:

```text
Discord user message
        |
        v
+-------------------+
| Dom Agent Session |
+-------------------+
   |     |      |
   |     |      +--> slash commands
   |     |
   |     +---------> tool calls
   |                  |
   |                  +--> files
   |                  +--> shell
   |                  +--> web/search
   |                  +--> Discord actions
   |                  +--> context compaction
   |                  +--> background workers
   |
   +--------------> reply to Discord


Heartbeat loop ----> internal prompt ----> dom-agent session
Cron wakeup -------> internal prompt ----> dom-agent session
Worker handoff ----> internal prompt ----> dom-agent session
```

The important design choice is this:

- the dom agent owns the relationship with the user
- workers, heartbeat, and wakeups feed structured events into that relationship

That keeps the bot feeling like one ongoing presence instead of a pile of unrelated agent messages.

## Main runtime pieces

### 1. Discord service layer

The Discord service is the outer shell of the app.

It is responsible for:

- receiving messages and slash commands
- resolving which session a message belongs to
- downloading attachments if enabled
- queueing prompts into the correct session
- sending replies back to Discord
- exposing public slash-command status

Main code:

- [internal/discordbot/service.go](../internal/discordbot/service.go)

### 2. Session state

Each active chat lives in a session state object.

That session stores:

- session key information
- current in-memory history
- a processing queue
- cancellation state
- timestamps
- persisted history on disk

The session key depends on your Discord scope configuration:

- `channel` scope means one shared session per channel
- `user` scope means one per user per channel or DM context

This is one of the most behavior-changing settings in the whole app.

## Request flow

When a user sends a message, the flow is roughly:

```text
user message
   |
   v
attachment rewrite
   |
   v
session lookup / create
   |
   v
system prompt assembly
   |
   v
history trimming
   |
   v
model call
   |
   +--> tool loop if needed
   |
   v
final assistant reply
   |
   v
history persistence
   |
   v
Discord reply
```

More concretely, Element Orion does this:

1. receives the Discord event
2. rewrites attachment links to local downloaded paths if that feature is enabled
3. finds the correct session
4. assembles the runtime prompt
5. estimates token usage and trims history to fit the input budget
6. runs the model
7. executes any tool calls
8. loops until it has a final assistant answer or hits loop/tool caps
9. persists compacted session history
10. sends the user-facing reply

## Prompt assembly

Element Orion does not rely on a tiny static system prompt alone.

It builds a larger prompt context from several sources.

### Runtime metadata

The startup prompt includes concrete runtime facts such as:

- app name
- workspace root
- model name
- reasoning effort
- context window
- reply budget
- local time
- UTC time
- enabled tools
- sandbox policy
- heartbeat settings
- background worker policy
- MCP server visibility

This matters because the model should know the environment it is actually operating inside.

### Workspace files

Element Orion can load files such as:

- `IDENTITY.md`
- `USER.md`
- `SOUL.md`
- `BOOTSTRAP.md`
- `CODEBASE.md`
- `TASKS.md`
- `HEARTBEAT.md`

These files act as durable runtime memory and operator intent.

### Skills

If skills are enabled, Element Orion snapshots available `SKILL.md` content into prompt context.

This is a powerful behavior lever because it changes not just what tools exist, but how the model is taught to use them.

### Memory shards

Element Orion also loads memory shards from disk.

That gives it a lightweight long-term memory mechanism that survives restarts and session churn.

## Context and history model

One of the most important distinctions in Element Orion is that several different "context" numbers exist at the same time.

They are not interchangeable.

### Stored session history

This is the durable chat history Element Orion keeps on disk and in the session state.

It can be compacted for storage continuity.

### Startup prompt payload

This is all the extra loaded material:

- system instructions
- runtime metadata
- workspace files
- skills
- memory shards

This may be quite large even before user chat history is added.

### Live model input

This is what the provider actually sees for the current turn after Element Orion:

- reserves reply tokens
- computes input budget
- loads startup material
- trims the conversation history

That is why `/status` needs to show more than just "messages in this chat."

## Compaction

Element Orion uses two related but different compaction ideas.

### Storage compaction

Stored session history can be summarized once it crosses configured thresholds.

This keeps continuity without carrying every old line forever.

Controlled by:

- `app.history_compaction.enabled`
- `app.history_compaction.trigger_tokens`
- `app.history_compaction.target_tokens`
- `app.history_compaction.preserve_recent_messages`

### In-turn compaction tool

The model can also call `compact_context`.

That is a tool-level move, not just a background storage behavior.

It helps when the agent notices:

- the working set is crowded
- too much old context is still hanging around
- the conversation needs a tighter summary before continuing

## Background workers

Background workers are separate sub-agent runs designed for long or noisy tasks.

### Spawn behavior

When the dom agent starts a worker, the worker gets:

- a snapshot of the current session history
- a copy of the current prompt setup
- its own task record
- its own event log

After spawn, the worker is separate.

That means:

- the main chat does not keep streaming into the worker
- the worker does not keep streaming its internal transcript back into the main chat

### Handoff behavior

When the worker finishes or fails:

- the worker does not directly message the user
- Element Orion creates an internal handoff event
- that handoff is queued into the dom-agent session
- the dom agent replies normally in its own voice

So the user sees one coherent companion, not raw worker boilerplate.

### Why not auto-merge the full worker transcript?

Because a full worker transcript can be:

- huge
- repetitive
- polluted with low-level tool chatter
- harmful to the main chat’s working context

Element Orion currently merges back the result through a structured handoff, not a raw transcript dump.

## Background task lifecycle

```text
Dom agent starts worker
        |
        v
task record created
        |
        v
worker runs model + tools
        |
        +--> event log grows
        +--> optional sandbox commands run
        |
        v
worker completes or fails
        |
        v
internal handoff prompt created
        |
        v
dom agent receives handoff
        |
        v
dom agent sends user-facing follow-up
```

Key idea:

- worker lane does work
- dom lane owns the conversation

## Heartbeat system

Heartbeat is a proactive system loop.

It is not a normal chat turn.

It exists so Element Orion can notice and act on things without waiting for a user message.

Examples:

- morning check-ins
- checklist follow-ups
- handling queued system events
- nudging itself to revisit a task

Heartbeat behavior is configurable:

- separate model or inherited model
- isolated session or shared session
- light context or full context
- active-hour window
- per-target delivery

This gives you a "caretaker lane" that can be quieter and more deliberate than a normal chat loop.

## Precise wakeups

Precise wakeups are one-shot scheduled events.

They are more exact than a repeating heartbeat interval.

Example use cases:

- "follow up tomorrow at 8:30"
- "check this again in two hours"
- "wake next Monday morning and remind me"

Internally, they are turned into queued events that the heartbeat/event processing system can deliver.

## Attachment handling

If incoming attachment download is enabled, Element Orion:

1. downloads the uploaded file
2. stores it locally
3. rewrites the prompt content so the agent sees the local path instead of only a Discord CDN URL

This is important because local tools can then open and inspect the file directly.

Without that step, the model might only see a link it cannot meaningfully use.

## Sandboxing

Sandboxing is optional and currently centered on Debian `systemd-nspawn`.

Important separation:

- worker = the background agent logic
- sandbox = where that worker’s shell commands execute

So a worker can exist without a sandbox, and a sandbox does not replace the worker.

When enabled, the worker can use a controlled Debian rootfs for system-level tasks.

That is useful for:

- distro-specific debugging
- package-manager experiments
- service layout inspection
- reproducible Linux-side testing

## Logs and observability

Element Orion is built to be inspectable.

You can inspect:

- background task status
- event logs
- tool output summaries
- latest worker summary in `/status`
- session history on disk
- uploaded files

This is a major philosophical difference from agent setups that act magical until they break.

## `/status` as an operator view

`/status` is meant to answer practical human questions:

- how full is the context window really?
- how much of that is prompt base versus live chat?
- is there a worker running?
- is the worker separate from this chat?
- how big was the worker spawn snapshot?
- what does merge-back actually mean here?

So the command tries to expose runtime truth in human words instead of dumping only raw counters.

## Failure modes and what they usually mean

### The model keeps feeling forgetful

Usually one or more of:

- too much startup prompt material
- too much stale history
- compaction disabled or badly tuned
- worker handoff too shallow for the task

### The bot keeps talking about time awkwardly

Usually one or more of:

- message timestamps are being injected into model-visible content
- the prompt taught the agent to repeat those timestamps back
- the reply formatting rules are too permissive

### A worker "finished" but the user saw ugly boilerplate

That was the old behavior Element Orion is moving away from.

The newer handoff model keeps worker results internal and lets the dom agent respond naturally.

### Sandboxed workers fail immediately

Usually one or more of:

- sandboxing was requested but not configured
- systemd/nspawn tools are missing
- root privileges or sudo are not available
- the host has low disk space
- the Debian rootfs bootstrap failed partway through

## Configuration philosophy

Element Orion works best when you treat config as behavior design, not just plumbing.

A few fields dramatically change how the agent feels:

- `discord.guild_session_scope`
- `llm.context_window_tokens`
- `app.history_compaction.*`
- `background_tasks.default_min_runtime`
- `background_tasks.sandbox.*`
- `heartbeat.*`
- `discord.download_incoming_attachments`

If the bot feels wrong, these settings are often a better place to look than immediately rewriting the prompt.

## Release mindset

If you want Element Orion to feel solid in production, aim for:

- safe example config committed
- real config ignored
- identity and private memory ignored
- heartbeat target configured intentionally
- compaction turned on
- worker tools enabled only as needed
- sandboxing configured only when the host can actually support it
- `/status` used as your first debugging surface

That is the mindset Element Orion is optimized for: companion behavior with operator visibility.
