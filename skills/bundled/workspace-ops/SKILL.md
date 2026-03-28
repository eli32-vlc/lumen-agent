---
name: workspace-ops
description: Guide for exploring and editing workspace files safely.
version: 1.0.0
metadata:
  openclaw:
    requires:
      bins:
        - rg
    primaryEnv: ""
---
Use this skill when you need to inspect, edit, and validate repository files.

Recommended flow:
1. List and search before editing.
2. Read target files fully enough to understand context.
3. Apply minimal changes.
4. Run tests or checks for touched behavior.
5. Summarize what changed and why.
