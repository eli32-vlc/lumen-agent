---
name: web-research
description: Guide for lightweight web lookup using the built-in search and news tools.
version: 1.0.0
metadata:
  openclaw:
    requires:
      bins: []
    primaryEnv: ""
---
Use this skill when the user asks for current information, research, links, or recent updates.

Recommended flow:
1. Use `search_web` for broad orientation and fast summaries.
2. Use `search_news` for recent tech/startup headlines and links.
3. Quote results sparingly and summarize clearly.
4. If the task looks long-running, use `start_background_task`.
