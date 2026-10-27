#!/bin/sh
# Shows the commit gate in a throwaway repository that is removed afterwards:
# a commit that breaks a lane is blocked, an edited test with no reason line is
# refused, and the same edit with the line goes through.
#
# Usage: sh examples/gate/demo.sh DIR
# DIR holds the built bindery-gate. Nothing outside the temporary directory is
# read or written, and no network is used.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
gate=$(cd "$1" && pwd)/bindery-gate
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# A repository the demo owns outright: no git settings from the user or the
# machine, and an invented author.
unset GIT_DIR GIT_INDEX_FILE GIT_WORK_TREE
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export BINDERY_GATE="$gate"

cd "$work"
git init -q -b main
git config user.name "Sample Author"
git config user.email "author@example.test"
cp "$here/lanes.tsv" lanes.tsv
cp "$here/../commit-msg" .git/hooks/commit-msg
chmod +x .git/hooks/commit-msg

# commit_expecting CODE MESSAGE... runs git commit and fails the demo when its
# exit code is not CODE. A refused commit exits 1, and the gate's lines appear
# on standard error above the exit line.
commit_expecting() {
	want=$1
	shift
	code=0
	git commit -q "$@" || code=$?
	echo "git commit exit $code"
	test "$code" -eq "$want"
}

echo "== A commit that passes both lanes (expect silence)"
printf 'ok\n' > status.txt
printf 'package widget\n' > widget_test.go
git add -- status.txt widget_test.go
echo '$ git commit -m "Add the status and a widget test"'
commit_expecting 0 -m "Add the status and a widget test"
echo

echo "== A commit that breaks the status lane (expect a block)"
printf 'broken\n' > status.txt
git add -- status.txt
echo '$ git commit -m "Mark the status broken"'
commit_expecting 1 -m "Mark the status broken"
printf 'ok\n' > status.txt
git add -- status.txt
echo

echo "== An edited test with no reason line (expect a refusal: a test loosened without a reason is how a regression gets a quiet pass)"
printf 'package widget\n\n// the expectation moved\n' > widget_test.go
git add -- widget_test.go
echo '$ git commit -m "Loosen the widget test"'
commit_expecting 1 -m "Loosen the widget test"
echo

echo "== The same edit with a reason line (expect silence)"
echo '$ git commit -m "Loosen the widget test" -m "Test changed: the old expectation was wrong"'
commit_expecting 0 -m "Loosen the widget test" -m "Test changed: the old expectation was wrong"
