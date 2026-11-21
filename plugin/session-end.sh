#!/bin/sh
# SessionEnd hook: tells the NeoSwitch daemon this session ended, so a project no other session uses goes back to Claude before the user can reopen it. Sessions not routed through a local proxy return here without starting NeoSwitch; a session that cannot reach the daemon is left to the daemon's own check.
case "${ANTHROPIC_BASE_URL:-}" in
http://127.0.0.1:*) ;;
*) exit 0 ;;
esac
"$(dirname -- "$0")/neoswitch.sh" --cached detach --pid "$1" 2>/dev/null || true
exit 0
