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
	case "${ANTHROPIC_BASE_URL:-}" in
	http://127.0.0.1:* | https://127.0.0.1:*) return 0 ;;
	esac
	state=$(neoswitch_state_dir)
	[ -d "$state" ] || return 1
	if grep -q 'https://127.0.0.1:' "${CLAUDE_PROJECT_DIR:-.}/.claude/settings.local.json" 2>/dev/null; then
		return 0
	fi
	[ -n "$1" ] && grep -q "\"$1\"" "$state/opt-ins.json" 2>/dev/null
}
