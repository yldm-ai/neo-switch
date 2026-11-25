---
description: Show whether this session uses Claude or Gemini 4 Neon, Google sign-in status, and quota usage
disable-model-invocation: true
allowed-tools: Bash(${CLAUDE_PLUGIN_ROOT}/plugin/neoswitch.sh *)
---

!`${CLAUDE_PLUGIN_ROOT}/plugin/neoswitch.sh status "${CLAUDE_PROJECT_DIR}"`

Tell the user the result above in their language, briefly. Keep any quota usage lines, percentages, reset times, and progress bars verbatim. Do not run any other command.
