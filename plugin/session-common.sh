# Shared by the hook scripts; sourced, not run. It finds NeoSwitch's state folder the way the binary does, reads the session id from the hook input, and keeps the session registry: one file per session, named after its id and holding its process id, which the daemon reads to place requests of sessions it has not met. Nothing is written where NeoSwitch never ran.
neoswitch_state_dir() {
	if [ -n "${NEOSWITCH_STATE_DIR:-}" ]; then
		printf '%s' "$NEOSWITCH_STATE_DIR"
		return
	fi
	case $(uname -s) in
	Darwin) printf '%s' "$HOME/Library/Application Support/neoswitch" ;;
	*) printf '%s' "${XDG_CONFIG_HOME:-$HOME/.config}/neoswitch" ;;
	esac
}

# neoswitch_session_id prints the session_id field of the hook input in $1; Claude Code's ids hold only letters, digits and hyphens.
neoswitch_session_id() {
	printf '%s' "$1" | sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9-]*\)".*/\1/p' | head -n 1
}

# neoswitch_register records session $1 as process $2.
neoswitch_register() {
	state=$(neoswitch_state_dir)
	if [ -n "$1" ] && [ -d "$state" ]; then
		mkdir -p "$state/sessions" 2>/dev/null && printf '%s\n' "$2" >"$state/sessions/$1" 2>/dev/null
	fi
	return 0
}

# neoswitch_routed reports whether this session's requests may go through NeoSwitch: its env names a local NeoSwitch address, the project's settings do, or the session opted in. A window that applied the settings after it started may not show them in a hook's env, so the settings file counts too.
neoswitch_routed() {
	state=$(neoswitch_state_dir)
	case "${ANTHROPIC_BASE_URL:-}" in
	https://127.0.0.1:*) return 0 ;;
	http://127.0.0.1:*)
		# A NeoSwitch from before TLS wrote this form with its token. Other local proxies use it too, and so do `neoswitch claude` and `neoswitch serve`, whose sessions this hook must leave alone.
		if [ -n "${ANTHROPIC_AUTH_TOKEN:-}" ] && grep -qF "\"token\": \"$ANTHROPIC_AUTH_TOKEN\"" "$state/proxy.json" 2>/dev/null; then
			return 0
		fi
		;;
	esac
	[ -d "$state" ] || return 1
	if grep -q 'https://127.0.0.1:' "${CLAUDE_PROJECT_DIR:-.}/.claude/settings.local.json" 2>/dev/null; then
		return 0
	fi
	[ -n "$1" ] && grep -q "\"$1\"" "$state/opt-ins.json" 2>/dev/null
}

# neoswitch_hint offers /neoswitch:on in a new session that does not use NeoSwitch, at most once a day per project. Only when the last check of NeoSwitch's route passed (the "ready" file /neoswitch:on and /neoswitch:login keep), never in a project where the user ran /neoswitch:off, never on resume, /clear or compaction, and never with NEOSWITCH_NO_HINT set. It uses the shell alone: a session that does not use NeoSwitch starts no NeoSwitch program.
neoswitch_hint() {
	[ -z "${NEOSWITCH_NO_HINT:-}" ] || return 0
	case $1 in
	*'"source":"startup"'* | *'"source": "startup"'*) ;;
	*) return 0 ;;
	esac
	state=$(neoswitch_state_dir)
	[ -f "$state/ready" ] || return 0
	project=${CLAUDE_PROJECT_DIR:-$(pwd)}
	if [ -f "$state/declined" ] && grep -Fxq "$project" "$state/declined" 2>/dev/null; then
		return 0
	fi
	today=$(date +%Y-%m-%d)
	marker="$state/hinted/$(printf '%s' "$project" | cksum | cut -d' ' -f1)"
	if [ "$(cat "$marker" 2>/dev/null)" = "$today" ]; then
		return 0
	fi
	mkdir -p "$state/hinted" 2>/dev/null && printf '%s\n' "$today" >"$marker" 2>/dev/null
	printf '%s\n' '{"systemMessage":"NeoSwitch is set up: /neoswitch:on answers this session with Gemini 4 Neon, and /neoswitch:off returns it to Claude any time."}'
	return 0
}
