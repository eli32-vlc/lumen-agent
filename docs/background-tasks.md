# Background Tasks

Background tasks are Lumen’s worker system.

They exist for work that is too long, too noisy, or too asynchronous for the foreground chat loop.

## The mental model

Think of Lumen as having two lanes:

- the dom agent lane
- the worker lane

The dom agent talks to the user.
The worker does the long-running job.

That distinction matters a lot.

## What happens when a worker starts

When the dom agent calls `start_background_task`, Lumen:

1. looks up the current channel session
2. copies the current session history
3. copies the current skill snapshot
4. creates a background-task record
5. starts the worker loop in its own goroutine

So the worker begins with a snapshot of the chat as it existed at spawn time.

After that:

- the worker keeps its own history
- the main chat keeps its own history
- they are no longer the same live context

That means the worker does inherit context, but only once at the start.

## Does worker context merge back?

Not fully.

Lumen now uses a handoff model instead of having the worker talk directly to the user.

When a worker finishes or fails:

- the worker does not send raw runtime boilerplate to the user
- Lumen creates an internal handoff event for the dom agent
- the dom agent gets that worker result automatically
- the dom agent then sends the user-facing reply

So merge-back looks like this:

- worker result and handoff: yes
- full worker transcript: no, not automatically
- raw worker status messages to the user: no

This keeps workers as workers and keeps user-facing replies owned by the dom agent.

## Worker handoff model

The internal handoff includes:

- worker status
- task ID
- original worker prompt
- worker spawn snapshot size
- current worker context size
- final worker reply or error

This gives the dom agent enough context to continue naturally without requiring the user to inspect worker logs manually.

## What `/status` shows

`/status` now shows:

- main chat context usage
- background job counts
- latest worker context summary for the channel
- whether that worker context is separate from the foreground chat
- whether merge-back is automatic

This is meant to answer the human question:

"what is the worker doing relative to this chat?"

instead of dumping raw runtime internals.

## Worker controls

### `start_background_task`

Starts a worker.

Key inputs:

- `prompt`
- `model_override`
- `light_context`
- `min_runtime`
- `sandbox`

### `list_background_tasks`

Lists recent worker tasks visible from the current channel context.

### `get_background_task`

Returns high-level task state.

Useful for:

- status
- timestamps
- result
- error
- sandbox info

### `get_background_task_logs`

Returns event logs and tool output summaries.

This is what the dom agent should use when the user asks:

- “what is the worker doing?”
- “what command is it on?”
- “why did it fail?”

### `cancel_background_task`

Cancels a running worker.

## Runtime events

Each worker records structured events such as:

- status updates
- assistant messages
- tool started
- tool finished

Events include:

- tool name
- short detail
- full detail
- timestamp

The event log is bounded by `background_tasks.max_event_log_entries`.

## Status lifecycle

A worker can be:

- `queued`
- `running`
- `completed`
- `failed`
- `canceled`

Each task also records:

- `created_at`
- `updated_at`
- `started_at`
- `completed_at`
- `min_runtime_seconds`

## Minimum runtime

`min_runtime` is a floor, not a target quality guarantee by itself.

The idea is:

- don’t let the worker stop after one shallow pass
- give it time to explore, verify, or write properly
- only let it stop early if it is genuinely blocked

This is especially useful for:

- codebase analysis
- longer web research
- artifact generation
- multi-step debugging

## Why workers help

Without workers, agents tend to:

- block the current chat
- give fake “still working” messages
- lose detail about what they were doing
- clutter the main chat with low-value intermediate updates

Lumen’s worker model is meant to avoid that.

## Sandboxed workers

If a task requests sandboxing, or sandboxing is globally forced, shell execution inside that worker is routed through a sandbox container.

See [Sandboxing](sandboxing.md).
