---
name: openclaw-skill-converter
description: Convert OpenClaw skills into Lumen Agent skills. Use when importing a skill folder or SKILL.md from OpenClaw and adapting it to Lumen's skill loader, workspace layout, and prompt conventions.
version: 1.0.0
metadata:
  openclaw:
    requires:
      bins:
        - rg
    primaryEnv: ""
---
Use this skill when the user wants to import, rewrite, or normalize an OpenClaw skill for Lumen Agent.

What upstream OpenClaw skills look like:
1. Each skill lives in its own folder under `skills/<skill-name>/`.
2. The required file is `SKILL.md`.
3. `SKILL.md` uses YAML frontmatter plus a markdown body.
4. Real upstream examples include:
   - Required/common fields: `name`, `description`
   - Often-present optional fields: `version`, `homepage`
   - OpenClaw metadata under `metadata.openclaw`
5. Real upstream `metadata.openclaw` examples include:
   - `emoji`
   - `requires.env`
   - `requires.bins`
   - `primaryEnv`
   - `install` entries for installation guidance
6. Optional companion folders can exist, such as `references/`, `scripts/`, `assets/`, and sometimes UI-oriented metadata in other ecosystems.

Lumen Agent compatibility rules:
1. Lumen already reads OpenClaw-style frontmatter from `SKILL.md`.
2. Preserve `name`, `description`, and `version` exactly unless the user asks to rename them.
3. Preserve `metadata.openclaw` when present. Lumen uses `metadata.openclaw.requires.env`, `metadata.openclaw.requires.bins`, and `primaryEnv` for filtering.
4. Also preserve extra OpenClaw metadata like `emoji` and `install` unless there is a clear reason to drop them.
5. Keep bundled `references/`, `scripts/`, and `assets/` when they are still useful in the Lumen workspace.
6. If the skill has UI-only files for another agent runtime, keep them only if the user wants archival fidelity; otherwise they can be omitted from the Lumen-focused copy.

Recommended conversion flow:
1. Inspect the source skill folder and identify every file that belongs to the skill.
2. Read `SKILL.md` first and preserve the frontmatter contract.
3. Rewrite the body only where Lumen-specific guidance is needed:
   - Replace OpenClaw-only commands or tool names with Lumen equivalents.
   - Update paths so they make sense in the current workspace.
   - Remove instructions that depend on unavailable runtimes, channels, or tools.
4. Keep the skill concise. Move large detailed references into `references/` files instead of bloating `SKILL.md`.
5. Save the converted skill under the target Lumen skills directory, usually `skills/<skill-name>/` in the workspace or bundled skills tree.
6. Validate the result by checking that:
   - `SKILL.md` exists
   - frontmatter still parses cleanly
   - the skill description still clearly explains when to use it
   - any required binaries or env vars are still accurate for Lumen

Conversion checklist:
1. Folder exists at `skills/<skill-name>/`
2. `SKILL.md` exists
3. Frontmatter preserved or intentionally normalized
4. OpenClaw metadata kept under `metadata.openclaw`
5. Lumen-incompatible commands rewritten or called out
6. Optional resources copied only when still useful
7. Final skill is concise and actionable for Lumen Agent

When in doubt:
1. Prefer lossless conversion first.
2. Preserve metadata unless it is wrong, unsafe, or clearly irrelevant.
3. State any assumptions if an OpenClaw skill references tools Lumen does not have.
