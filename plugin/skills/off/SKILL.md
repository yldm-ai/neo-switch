---
description: Switch this session back to Claude from its next message, with your own Claude login
disable-model-invocation: true
allowed-tools: Bash(${CLAUDE_PLUGIN_ROOT}/plugin/neoswitch.sh *)
---

!`${CLAUDE_PLUGIN_ROOT}/plugin/neoswitch.sh off "${CLAUDE_PROJECT_DIR}"`

Tell the user the result above in their language, briefly. Do not run any other command.
