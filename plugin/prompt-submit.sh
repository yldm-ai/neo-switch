#!/bin/sh
# UserPromptSubmit hook: before a prompt of a session whose requests go through NeoSwitch, make sure the daemon answers, starting it again if it stopped, for example after a crash, or after the last session it knew about ended while this window still used it. It prints nothing: this hook's output would reach the model. Sessions that do not use NeoSwitch return here after a few shell tests.
. "$(dirname -- "$0")/session-common.sh"
id=$(neoswitch_session_id "$(cat)")
neoswitch_register "$id" "$1"
neoswitch_routed "$id" || exit 0
"$(dirname -- "$0")/neoswitch.sh" --cached attach --quiet --pid "$1" --session "$id" >/dev/null 2>&1 </dev/null || true
exit 0
