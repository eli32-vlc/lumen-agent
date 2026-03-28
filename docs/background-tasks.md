# Background Tasks

Background tasks are Lumen’s sub-agent system.

They exist for work that is too long, too noisy, or too asynchronous for the main foreground reply loop.

## What they solve

Without background tasks, agents tend to do one of two bad things:

- block the current conversation while doing long work
- pretend they are still working without a reliable way to inspect what they are doing

Lumen’s background-task model is built to avoid both.

## Core behavior

When the Dom Agent starts a task with `start_background_task`, the runtime can carry over the current runtime history/context and run the task independently.

The task can also be given:

- `min_runtime`: a minimum wall-clock time budget
- `model_override`
- `light_context`
- `sandbox`

If `min_runtime` is set, the task should keep working until it reaches that floor or is genuinely blocked. It is meant to discourage low-effort early exits.

## Tools

### `start_background_task`

Starts a background sub-agent.

Key inputs:

- `prompt`
- `model_override`
- `light_context`
- `min_runtime`
- `sandbox`

### `list_background_tasks`

Lists tasks, optionally filtered by status.

### `get_background_task`

Returns task status, timestamps, result, error, and sandbox info. It can also include recent events when `include_events` is true.

### `get_background_task_logs`

Returns detailed event logs, including tool-call output and command output summaries.

This is the tool the Dom Agent should use when a user asks what a sub-agent is doing.

### `cancel_background_task`

Cancels a running task.

## Event model

Each task records structured events such as:

- status updates
- assistant messages
- tool started
- tool finished

Events can include:

- tool name
- short detail
- full detail
- timestamp

The in-memory event log uses a bounded ring buffer controlled by `background_tasks.max_event_log_entries`.

## Task status model

A task can be:

- `queued`
- `running`
- `completed`
- `failed`
- `canceled`

The runtime also records:

- `created_at`
- `updated_at`
- `started_at`
- `completed_at`
- `min_runtime_seconds`

## Why this matters

The main improvement here is operational visibility.

If a user asks:

- “what is the sub-agent doing?”
- “what command is it stuck on?”
- “what was the last tool output?”

the Dom Agent can answer using task state and logs instead of guessing.

## Context inheritance

Background tasks can inherit the current runtime history from the Dom Agent. This helps reduce the “sub-agent forgot everything” problem and gives the worker a better starting context.

## Sandboxed tasks

If a task requests sandboxing, or sandboxing is globally forced, shell execution inside that background task is routed through a sandbox container.

See [Sandboxing](sandboxing.md).
