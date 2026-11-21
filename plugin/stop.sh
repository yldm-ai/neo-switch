#!/bin/sh
# Stop hook: checks that a reply showing NeoSwitch's Google sign-in link shows it exactly (see checkRelayedLink). Almost no reply carries that link, so those return here without starting NeoSwitch. The hook never fails the turn: a problem in the check only skips it.
input=$(cat)
case $input in
*accounts.google.com*) ;;
*) exit 0 ;;
esac
printf '%s' "$input" | "$(dirname -- "$0")/neoswitch.sh" --cached sign-in-check 2>/dev/null || true
exit 0
