#!/bin/sh
# SessionEnd hook: tells the NeoSwitch daemon this session ended, so a project no other session uses goes back to Claude before the user can reopen it. Sessions not routed through a local proxy return here without starting NeoSwitch; a session that cannot reach the daemon is left to the daemon's own check.
. "$(dirname -- "$0")/session-common.sh"
input=$(cat)
id=$(neoswitch_session_id "$input")
neoswitch_routed "$id" || exit 0
printf '%s' "$input" | "$(dirname -- "$0")/neoswitch.sh" --cached detach --pid "$1" >/dev/null 2>&1 || true
exit 0
