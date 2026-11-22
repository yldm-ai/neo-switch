#!/bin/sh
# Checks the release invariant on a pull request's merge commit (HEAD; its first parent is the base branch):
# the plugin marketplace pins the plugin to the tag named by VERSION, and when the pin moves, that tag's release is
# published and was built from a tree whose VERSION says the same. Users get the plugin from the pinned tag, never
# from main, so the plugin source and the release binary a user without Go downloads always come from one tree.
# The update notice of earlier binaries reads main's VERSION, which this keeps equal to what an update delivers.
# Usage: released-version.sh <owner/repo>
set -eu
repo=$1
marketplace=.claude-plugin/marketplace.json

pin_of() {
	jq -r --arg repo "$repo" '.plugins[] | select(.name == "neoswitch") | .source
		| if type == "object" and .source == "github" and .repo == $repo then (.ref // "") else "" end'
}

version="v$(tr -d ' \t\r\n' <VERSION)"
pin=$(pin_of <"$marketplace")
if [ "$pin" != "$version" ]; then
	echo "::error::$marketplace must pin the plugin to {\"source\": \"github\", \"repo\": \"$repo\", \"ref\": \"$version\"}, the tag VERSION names, but it pins \"${pin:-nothing}\". A release changes VERSION and the pin together (see CONTRIBUTING.md)."
	exit 1
fi
base_pin=$(git show "HEAD^1:$marketplace" | pin_of || true)
if [ "$pin" = "$base_pin" ]; then
	echo "The plugin stays pinned to $pin."
	exit 0
fi
if ! git fetch --depth=1 origin "refs/tags/$version:refs/tags/$version"; then
	echo "::error::The pin moves to $version, but tag $version does not exist. Push the tag on this branch's head and wait for the release workflow before merging."
	exit 1
fi
tagged="v$(git show "$version:VERSION" | tr -d ' \t\r\n')"
if [ "$tagged" != "$version" ]; then
	echo "::error::Tag $version was made on a tree whose VERSION says $tagged, so its plugin would download the wrong release. Tag a commit whose VERSION matches."
	exit 1
fi
if ! curl -fsSIL -o /dev/null "https://github.com/$repo/releases/download/$version/checksums.txt"; then
	echo "::error::Tag $version exists, but its release is not published yet. Wait for the release workflow, then re-run this check."
	exit 1
fi
echo "The plugin moves to $version, a published release."
