#!/bin/sh
# UserPromptSubmit hook: before a prompt of a session whose requests go through NeoSwitch, make sure the daemon answers, starting it again if it stopped, for example after a crash, or after the last session it knew about ended while this window still used it. Sessions that do not use NeoSwitch return here after a few shell tests.
#
# This hook's plain output would reach the model, so only a systemMessage, which Claude Code shows the user, gets through: once per session, about auto mode in a session that Gemini answers (cmd/neoswitch/automode.go).
. "$(dirname -- "$0")/session-common.sh"
input=$(cat)
id=$(neoswitch_session_id "$input")
neoswitch_register "$id" "$1"
neoswitch_routed "$id" || exit 0
mode=$(printf '%s' "$input" | sed -n 's/.*"permission_mode"[[:space:]]*:[[:space:]]*"\([A-Za-z]*\)".*/\1/p' | head -n 1)
note=$("$(dirname -- "$0")/neoswitch.sh" --cached attach --quiet --pid "$1" --session "$id" ${mode:+--mode "$mode"} 2>/dev/null </dev/null) || true
case $note in
'{"systemMessage":'*) printf '%s\n' "$note" ;;
esac
exit 0
