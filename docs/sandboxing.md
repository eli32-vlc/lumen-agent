# Sandboxing

Lumen supports optional sandboxed execution for background tasks using Debian `systemd-nspawn` containers.

This is not meant to sandbox every foreground action by default. It is specifically designed for background-task execution and explicit container lifecycle management.

## Design goal

The goal is to give the Dom Agent a real isolation path for risky or self-contained background work while still letting the runtime inspect and manage that environment.

## How it works

When sandboxing is enabled for a background task:

1. the runtime creates or reuses a Debian rootfs under `background_tasks.sandbox.machines_dir`
2. the container is managed with `systemd-nspawn`
3. the workspace is bind-mounted into the container
4. background-task shell execution is run through `systemd-run --machine=...`

In practice, that means the worker can do Linux-style system work in a cleaner environment while the dom agent still stays on the host runtime.

## Worker vs sandbox

These are different layers:

- worker = background sub-agent runtime
- sandbox = optional execution environment for that worker’s shell actions

So a worker can exist without a sandbox.

When sandboxing is enabled, the worker still owns the job, but shell execution goes through the container.

## Why this is useful

Real uses include:

- reproducing Debian-only bugs
- testing package installation steps
- checking filesystem layout assumptions
- installing build dependencies without polluting the host
- running messier system experiments in a disposable rootfs

The current implementation is aligned with:

- [`systemd-nspawn`](https://www.freedesktop.org/software/systemd/man/latest/systemd-nspawn.html)
- [`machinectl`](https://www.freedesktop.org/software/systemd/man/latest/machinectl.html)
- [`systemd-run`](https://www.freedesktop.org/software/systemd/man/latest/systemd-run.html)
- [`org.freedesktop.machine1`](https://www.freedesktop.org/software/systemd/man/org.freedesktop.machine1.html)

## Config model

Relevant config fields:

- `background_tasks.sandbox.enabled`
- `background_tasks.sandbox.force`
- `background_tasks.sandbox.use_sudo`
- `background_tasks.sandbox.provider`
- `background_tasks.sandbox.release`
- `background_tasks.sandbox.architecture`
- `background_tasks.sandbox.mirror`
- `background_tasks.sandbox.machines_dir`
- `background_tasks.sandbox.setup_timeout`
- `background_tasks.sandbox.auto_cleanup`

## Enablement rules

### Opt-in mode

If `background_tasks.sandbox.enabled` is true and `force` is false:

- sandboxing is available
- the Dom Agent can request it per background task
- tasks that do not request it run normally
- if `use_sudo` is enabled, privileged sandbox commands are run through `sudo`

### Forced mode

If `background_tasks.sandbox.force` is true:

- sandboxing is enabled automatically
- every background task uses sandboxed shell execution

### Off mode

If sandboxing is not enabled:

- background tasks run without containerized shell execution

## Lifecycle tools

The Dom Agent can manage sandboxes with:

- `list_sandbox_containers`
- `inspect_sandbox_container`
- `create_sandbox_container`
- `start_sandbox_container`
- `stop_sandbox_container`
- `delete_sandbox_container`

These tools are exposed so the agent can inspect and control the container lifecycle directly instead of treating the sandbox system as hidden runtime magic.

That makes sandboxing visible and debuggable:

- which machines exist
- which machine a worker used
- whether it is still running
- where the rootfs lives

## Runtime behavior

### Create

`create_sandbox_container` bootstraps a Debian rootfs, using configured release and architecture defaults unless overridden.

### Start

`start_sandbox_container` launches the machine with `systemd-nspawn --boot`.

### Inspect

`inspect_sandbox_container` reports the known container state and rootfs location.

### Stop

`stop_sandbox_container` attempts graceful shutdown with `machinectl terminate`, with an optional force path.

### Delete

`delete_sandbox_container` removes the sandbox and its rootfs directory.

## Practical caveats

- This feature is Linux-oriented. It depends on systemd tooling.
- `debootstrap`, `systemd-nspawn`, `machinectl`, and `systemd-run` need to exist on the host.
- Creating or starting sandboxes usually requires running the Lumen service with root privileges.
- If `background_tasks.sandbox.use_sudo` is enabled, Lumen will try to run those privileged sandbox commands via `sudo`, which works best with passwordless sudo.
- If the host has low disk space or missing packages, sandbox creation can fail.
- A half-created rootfs can leave a container directory that exists but is not usable.

Also remember:

- sandboxing does not automatically make prompts smarter
- sandboxing does not automatically merge worker context back into the dom agent
- sandboxing changes where commands run, not who owns the user-facing reply

## Why this matters

The point is not just isolation. The point is controlled isolation.

Lumen gives the Dom Agent both:

- a way to run background shell commands in a container
- a way to inspect and manage that container lifecycle from the same runtime

That makes sandboxing an operational feature, not a marketing bullet.
