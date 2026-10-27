#!/bin/sh
# Shows the message bus between two named sessions: cobalt waits in the
# background, juniper tells it something, and cobalt's wait prints the message.
#
# Usage: sh examples/bus/demo.sh DIR
# DIR holds the built bindery-bus. The bus directory is a temporary one that is
# removed afterwards. Nothing outside it is read or written, and no network is
# used.
set -eu

bus=$(cd "$1" && pwd)/bindery-bus
work=$(mktemp -d)
pid=
cleanup() {
	if [ -n "$pid" ]; then kill "$pid" 2>/dev/null || true; fi
	rm -rf "$work"
}
trap cleanup EXIT

# mktemp -d makes a directory only this user can reach, which the bus requires.
export BINDERY_BUS_DIR="$work/bus"

echo "== cobalt waits for a message, in the background"
echo '$ bindery-bus wait --timeout 10s cobalt &'
"$bus" wait --timeout 10s cobalt > "$work/received" &
pid=$!

# The wait lists itself in who as soon as it starts looking.
tries=0
until "$bus" who 2>/dev/null | grep -q '^cobalt'; do
	tries=$((tries + 1))
	test "$tries" -lt 500
	sleep 0.01
done
echo
echo "== juniper tells cobalt"
echo '$ bindery-bus tell juniper cobalt "the build is green, ship it"'
"$bus" tell juniper cobalt "the build is green, ship it"

code=0
wait "$pid" || code=$?
pid=
echo
echo "== cobalt's wait returned it (expect exit 0 and the message)"
cat "$work/received"
echo "wait exit $code"
test "$code" -eq 0
grep -qx '#1 juniper: the build is green, ship it' "$work/received"

echo
echo "== A message is returned once: a second wait finds nothing (expect exit 1)"
echo '$ bindery-bus wait --timeout 0s cobalt'
code=0
"$bus" wait --timeout 0s cobalt || code=$?
echo "wait exit $code"
test "$code" -eq 1

echo
echo "== who lists the names, with what each last did"
echo '$ bindery-bus who'
"$bus" who
