#!/bin/sh
# SessionStart hook. Claude Code waits for it before the first model request, so when this project is routed through NeoSwitch the shared daemon is running (and this session registered with it) before it is needed. Sessions not routed through a local proxy return immediately without building anything.
. "$(dirname -- "$0")/session-common.sh"
id=$(neoswitch_session_id "$(cat)")
neoswitch_register "$id" "$1"
neoswitch_routed "$id" || exit 0
# attach reports its own problems as a systemMessage. When neoswitch.sh itself fails (no Go and no download, a refused checksum), Claude Code would only keep its stderr, which the user does not see, so the last line of it becomes the systemMessage instead. The hook always exits 0 so Claude Code reads that message.
errors=$(mktemp "${TMPDIR:-/tmp}/neoswitch-hook.XXXXXX") || exit 0
trap 'rm -f "$errors"' EXIT
if output=$("$(dirname -- "$0")/neoswitch.sh" attach --pid "$1" --session "$id" 2>"$errors"); then
	cat "$errors" >&2
	if [ -n "$output" ]; then
		printf '%s\n' "$output"
	fi
	exit 0
fi
cat "$errors" >&2
reason=$(awk 'NF { last = $0 } END { print last }' "$errors" | sed 's/^neoswitch: //; s/[.]$//; s/\\/\\\\/g; s/"/\\"/g' | tr -d '\000-\037')
printf '{"systemMessage":"NeoSwitch could not start, so this session cannot answer (%s). Run /neoswitch:off and start a new session to go back to Claude."}\n' "${reason:-no reason given}"
exit 0
