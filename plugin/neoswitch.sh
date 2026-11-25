#!/bin/sh
# Runs the NeoSwitch CLI bundled with this plugin. When Go is available, the binary is built from the plugin's own Go source on first use (and rebuilt when the source is newer) into the user's cache directory, which always matches the installed plugin exactly.
# Without Go, the script downloads the release binary named by the repository's VERSION file for this OS and architecture, verifies it against the release's checksums.txt and caches it per version; when the download fails, it runs the previously downloaded version if one is cached. The marketplace pins the plugin to a release tag, so the plugin source is always the tree that release binary was built from.
set -eu
# Tells the binary it runs from the plugin, which is what its update notice tells the user to update.
export NEOSWITCH_PLUGIN=1
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cache="${XDG_CACHE_HOME:-$HOME/.cache}/neoswitch"
bin="$cache/neoswitch-$(printf '%s' "$root" | cksum | cut -d' ' -f1)"
# Hooks that run after every reply or as a session ends pass --cached: they run this plugin's binary when it is here, otherwise the latest release cached, even one older than the plugin source, and otherwise do nothing, rather than build or download in the middle of the user's session. The latest matters for a window whose plugin was updated meanwhile: its hooks then start the takeover that moves every open window to the update (docs/design-session-routing.md, "Updates").
# latest_cached prints the latest release among the binaries in the cache, or nothing.
latest_cached() {
	latest=
	for cached in "$cache"/neoswitch-*; do
		case $cached in *.tmp) continue ;; esac
		if [ -f "$cached" ] && [ -x "$cached" ]; then
			# A binary that predates is-newer fails it, so a later release is never passed over for it.
			if [ -z "$latest" ] || "$cached" is-newer "$("$cached" version 2>/dev/null | cut -d' ' -f2)" "$("$latest" version 2>/dev/null | cut -d' ' -f2)" 2>/dev/null; then
				latest=$cached
			fi
		fi
	done
	printf '%s' "$latest"
}
if [ "${1:-}" = "--cached" ]; then
	shift
	if [ -f "$bin" ] && [ -x "$bin" ]; then
		exec "$bin" "$@"
	fi
	latest=$(latest_cached)
	if [ -n "$latest" ]; then
		exec "$latest" "$@"
	fi
	exit 0
fi
current() {
	[ -x "$bin" ] && [ -z "$(find "$root/cmd" "$root/internal" "$root/go.mod" "$root/go.sum" -newer "$bin" \( -name '*.go' -o -name 'go.*' \) -print 2>/dev/null | head -n 1)" ]
}
# build_failed marks a source the installed Go could not build, so later runs download the prebuilt release instead of failing the same build again; a change to the source tries again.
build_failed="$bin.build-failed"
builds() {
	command -v go >/dev/null 2>&1 || return 1
	[ -f "$build_failed" ] || return 0
	[ -n "$(find "$root/cmd" "$root/internal" "$root/go.mod" "$root/go.sum" -newer "$build_failed" \( -name '*.go' -o -name 'go.*' \) -print 2>/dev/null | head -n 1)" ]
}
version=v$(tr -d ' \t\r\n' <"$root/VERSION")
# --ready exits 0 when a run would start at once, and 1 when it would first build or download NeoSwitch, so the plugin's mod can say so before a wait of up to a minute.
if [ "${1:-}" = "--ready" ]; then
	if current; then
		exit 0
	fi
	if ! builds; then
		for downloaded in "$cache"/neoswitch-"$version"-*; do
			case $downloaded in *.tmp) continue ;; esac
			if [ -x "$downloaded" ]; then
				exit 0
			fi
		done
	fi
	exit 1
fi
if current; then
	exec "$bin" "$@"
fi
preparing="NeoSwitch: getting ready after an install or update; this can take up to a minute."
# removal is where the README says how to turn NeoSwitch off or remove it by hand.
removal="https://github.com/yldm-ai/neo-switch#turn-it-off-update-or-remove"

# unavailable reports why this plugin's NeoSwitch cannot run ($1). A NeoSwitch that ran here before is still in the cache, and an earlier version beats none: the user's way back, /neoswitch:off, must not depend on what just failed. The line starting "NeoSwitch fallback:" says so, and the session start hook shows it, since the earlier version cannot know it runs in place of a later one. Without any, /neoswitch:off says how to take NeoSwitch out of the project by hand, since only a NeoSwitch that ran can have put it there.
unavailable() {
	reason=$1
	shift
	latest=$(latest_cached)
	if [ -n "$latest" ]; then
		echo "NeoSwitch fallback: $reason Until then it runs the earlier $("$latest" version 2>/dev/null || basename "$latest") it ran before." >&2
		exec "$latest" "$@"
	fi
	echo "$reason" >&2
	if [ "${subcommand:-}" = off ]; then
		settings="${project:-.}/.claude/settings.local.json"
		echo "NeoSwitch could not run, so it did not switch anything off: $reason"
		echo "To take NeoSwitch out of this project by hand, open $settings and change its \"env\" object:"
		echo "- Delete ANTHROPIC_BASE_URL when it starts with https://127.0.0.1: (or, from earlier versions, http://127.0.0.1:, together with the ANTHROPIC_AUTH_TOKEN next to it)."
		echo "- In ANTHROPIC_CUSTOM_HEADERS, delete the X-NeoSwitch-Token line, and the whole entry when nothing else is left."
		echo "- Delete NODE_EXTRA_CA_CERTS when it names tls/bundle.pem in NeoSwitch's folder; if you had a CA file of your own there, put its path back instead."
		echo "- Delete CLAUDE_CODE_GATEWAY_HINT_HEADERS set to 1 and CLAUDE_CODE_USE_GATEWAY set to 0, unless you set them yourself."
		echo "Then start a new Claude Code session: it uses Claude."
		echo "To remove NeoSwitch completely, follow $removal"
		exit 0
	fi
	exit 1
}
subcommand=${1:-}
project=${2:-}

# prune removes the binaries of other plugin versions from the cache, keeping $1, the one about to run, builds or downloads still in progress, and binaries of later releases: a window still running an earlier plugin rebuilds its own binary when the user clears or resumes a session there, and removing the update then would put every window's hooks back on the earlier release. A daemon started from a removed binary keeps running until a later release takes its port over (see docs/design-session-routing.md, "Updates").
prune() {
	keep=$("$1" version 2>/dev/null | cut -d' ' -f2)
	for old in "$cache"/neoswitch-*; do
		case $old in
		"$1" | *.tmp | *.build-failed) ;;
		*)
			if [ -f "$old" ] && ! "$1" is-newer "$("$old" version 2>/dev/null | cut -d' ' -f2)" "$keep" 2>/dev/null; then
				rm -f "$old"
			fi
			;;
		esac
	done
}

if builds; then
	mkdir -p "$cache"
	echo "$preparing" >&2
	# The version names the plugin's source: the release it follows plus the plugin directory, which Claude Code names after the installed version or commit.
	plugin_dir=$(printf '%s' "$(basename "$root")" | tr -c 'A-Za-z0-9._-' '_')
	if (cd "$root" && go build -trimpath -ldflags "-X main.version=$version+source.$plugin_dir" -o "$bin.tmp" ./cmd/neoswitch) >&2; then
		rm -f "$build_failed"
		mv "$bin.tmp" "$bin"
		prune "$bin"
		exec "$bin" "$@"
	fi
	# The marketplace pins the plugin to a release, so the prebuilt binary of that release is the same NeoSwitch.
	rm -f "$bin.tmp"
	go version >"$build_failed" 2>/dev/null || : >"$build_failed"
	echo "NeoSwitch could not build itself with $(go version 2>/dev/null || echo the installed Go), so it uses the prebuilt NeoSwitch $version." >&2
fi

case $(uname -s) in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) os= ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) arch= ;;
esac
if [ -z "$os" ] || [ -z "$arch" ]; then
	unavailable "NeoSwitch has no prebuilt binary for $(uname -s) $(uname -m); install Go from https://go.dev/dl so the plugin can build it, then try again." "$@"
fi
release="$cache/neoswitch-$version-${os}_$arch"
if [ ! -x "$release" ]; then
	if command -v sha256sum >/dev/null 2>&1; then
		sha256() { sha256sum "$1" | cut -d' ' -f1; }
	elif command -v shasum >/dev/null 2>&1; then
		sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
	else
		unavailable "NeoSwitch cannot verify the prebuilt binary because neither sha256sum nor shasum is installed; install Go from https://go.dev/dl so the plugin can build it, then try again." "$@"
	fi
	asset="neoswitch_${version}_${os}_$arch"
	base="https://github.com/yldm-ai/neo-switch/releases/download/$version"
	mkdir -p "$cache"
	tmp=$(mktemp -d "$cache/.download.XXXXXX")
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' INT TERM
	echo "$preparing" >&2
	echo "NeoSwitch: downloading $asset ..." >&2
	if ! curl -fsSL --connect-timeout 15 --max-time 300 -o "$tmp/checksums.txt" "$base/checksums.txt" || ! curl -fsSL --connect-timeout 15 --max-time 300 -o "$tmp/$asset" "$base/$asset"; then
		# An update installed while offline would otherwise stop every session until the network is back. prune only runs after a verified download, so the previous version's binary is still cached; it runs until the download succeeds.
		rm -rf "$tmp"
		trap - EXIT INT TERM
		unavailable "NeoSwitch could not download $asset from $base; check the network, or install Go from https://go.dev/dl so the plugin can build it. It tries again next time." "$@"
	fi
	want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")
	got=$(sha256 "$tmp/$asset")
	if [ -z "$want" ] || [ "$want" != "$got" ]; then
		rm -rf "$tmp"
		trap - EXIT INT TERM
		unavailable "NeoSwitch refused the downloaded $asset: its SHA-256 ($got) does not match checksums.txt (${want:-no entry}), so it was not run." "$@"
	fi
	chmod +x "$tmp/$asset"
	mv "$tmp/$asset" "$release"
	rm -rf "$tmp"
	trap - EXIT INT TERM
	echo "NeoSwitch: verified $asset." >&2
	prune "$release"
fi
exec "$release" "$@"
