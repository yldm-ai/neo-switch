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
if [ "${1:-}" = "--cached" ]; then
	shift
	if [ -f "$bin" ] && [ -x "$bin" ]; then
		exec "$bin" "$@"
	fi
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
	if [ -n "$latest" ]; then
		exec "$latest" "$@"
	fi
	exit 0
fi
if [ -x "$bin" ] && [ -z "$(find "$root/cmd" "$root/internal" "$root/go.mod" "$root/go.sum" -newer "$bin" \( -name '*.go' -o -name 'go.*' \) -print 2>/dev/null | head -n 1)" ]; then
	exec "$bin" "$@"
fi
version=v$(tr -d ' \t\r\n' <"$root/VERSION")
preparing="NeoSwitch: getting ready after an install or update; this can take up to a minute."

# prune removes the binaries of other plugin versions from the cache, keeping $1, the one about to run, builds or downloads still in progress, and binaries of later releases: a window still running an earlier plugin rebuilds its own binary when the user clears or resumes a session there, and removing the update then would put every window's hooks back on the earlier release. A daemon started from a removed binary keeps running until a later release takes its port over (see docs/design-session-routing.md, "Updates").
prune() {
	keep=$("$1" version 2>/dev/null | cut -d' ' -f2)
	for old in "$cache"/neoswitch-*; do
		case $old in
		"$1" | *.tmp) ;;
		*)
			if [ -f "$old" ] && ! "$1" is-newer "$("$old" version 2>/dev/null | cut -d' ' -f2)" "$keep" 2>/dev/null; then
				rm -f "$old"
			fi
			;;
		esac
	done
}

if command -v go >/dev/null 2>&1; then
	mkdir -p "$cache"
	echo "$preparing" >&2
	# The version names the plugin's source: the release it follows plus the plugin directory, which Claude Code names after the installed version or commit.
	plugin_dir=$(printf '%s' "$(basename "$root")" | tr -c 'A-Za-z0-9._-' '_')
	(cd "$root" && go build -trimpath -ldflags "-X main.version=$version+source.$plugin_dir" -o "$bin.tmp" ./cmd/neoswitch) >&2
	mv "$bin.tmp" "$bin"
	prune "$bin"
	exec "$bin" "$@"
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
	echo "NeoSwitch has no prebuilt binary for $(uname -s) $(uname -m); install Go from https://go.dev/dl so the plugin can build it, then try again." >&2
	exit 1
fi
release="$cache/neoswitch-$version-${os}_$arch"
if [ ! -x "$release" ]; then
	if command -v sha256sum >/dev/null 2>&1; then
		sha256() { sha256sum "$1" | cut -d' ' -f1; }
	elif command -v shasum >/dev/null 2>&1; then
		sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
	else
		echo "NeoSwitch cannot verify the prebuilt binary because neither sha256sum nor shasum is installed; install Go from https://go.dev/dl so the plugin can build it, then try again." >&2
		exit 1
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
		# An update installed while offline would otherwise stop every session until the network is back. prune only runs after a verified download, so the previous version's verified binary is still cached; run it until the download succeeds.
		for previous in "$cache"/neoswitch-v*-"${os}_$arch"; do
			if [ -x "$previous" ]; then
				echo "NeoSwitch could not download $asset from $base, so it runs the version downloaded earlier ($(basename "$previous")) and tries again next time." >&2
				rm -rf "$tmp"
				trap - EXIT INT TERM
				exec "$previous" "$@"
			fi
		done
		echo "NeoSwitch could not download $asset from $base; check the network, or install Go from https://go.dev/dl so the plugin can build it." >&2
		exit 1
	fi
	want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")
	got=$(sha256 "$tmp/$asset")
	if [ -z "$want" ] || [ "$want" != "$got" ]; then
		echo "NeoSwitch refused the downloaded $asset: its SHA-256 ($got) does not match checksums.txt (${want:-no entry}). Nothing was run." >&2
		exit 1
	fi
	chmod +x "$tmp/$asset"
	mv "$tmp/$asset" "$release"
	rm -rf "$tmp"
	trap - EXIT INT TERM
	echo "NeoSwitch: verified $asset." >&2
	prune "$release"
fi
exec "$release" "$@"
