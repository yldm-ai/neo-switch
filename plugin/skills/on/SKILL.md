---
description: Switch this session to Gemini 4 Neon from its next message, once you have signed in or set up a Gemini API key. Other sessions keep using Claude
disable-model-invocation: true
allowed-tools: Bash(${CLAUDE_PLUGIN_ROOT}/plugin/neoswitch.sh *)
---

!`${CLAUDE_PLUGIN_ROOT}/plugin/neoswitch.sh on "${CLAUDE_PROJECT_DIR}"`

Tell the user the result above in their language, briefly. Do not run any other command.
