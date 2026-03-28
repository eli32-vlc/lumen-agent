---
name: weather
description: Guide for weather lookups using the built-in free weather tool.
version: 1.0.0
metadata:
  openclaw:
    requires:
      bins: []
    primaryEnv: ""
---
Use this skill when the user asks about current weather or a short forecast.

Recommended flow:
1. Use `get_weather` with the location the user provided.
2. Summarize current conditions first.
3. Include forecast highs/lows only when they help.
4. If the location is ambiguous, say which match you used.
