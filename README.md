# bindery

[![CI](https://github.com/kingletas/bindery/actions/workflows/ci.yml/badge.svg)](https://github.com/kingletas/bindery/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Bindery is a small, runnable showcase of a multi-session AI coding harness: guards that read every tool call before it runs, a commit gate that maps changed paths to lanes, and a message bus between named sessions. It is written in Go, on the standard library plus one shell parser, and every name, path and host in it is invented.

> **Status: the guards, the commit gate and the message bus are built**, and `make demo` shows all three on invented data.

## What it claims, and where to check

Each row is a claim about this showcase, not about any other system. A row gets a file and a test when the code exists, and not before.

| Claim | File | Test |
|---|---|---|
| A guard refuses a destructive command and says why | [recursive_delete.go](internal/guard/recursive_delete.go), [force_push.go](internal/guard/force_push.go), [pipe_to_shell.go](internal/guard/pipe_to_shell.go), [secrets_read.go](internal/guard/secrets_read.go) | `TestRecursiveDeleteRefusesAProtectedOrOutsidePath`, `TestForcePushRefusesAProtectedBranch`, `TestPipeToShellRefusesADownloadThatIsRun`, `TestSecretsReadRefusesAConfiguredSecretsPath`, and `TestARefusedCallExitsTwoAndNamesTheGuardOnStderr` for the runner |
| The same guard is silent on a harmless command | the same four files | `TestRecursiveDeleteIsSilentOnHarmlessCommands`, `TestForcePushIsSilentOnHarmlessCommands`, `TestPipeToShellIsSilentOnHarmlessCommands`, `TestSecretsReadIsSilentOnHarmlessCommands`, and `TestASilentCallExitsZeroAndPrintsNothing` for the runner |
| A guard fails closed on a line it cannot parse | [shellscan.go](internal/shellscan/shellscan.go), [guard.go](internal/guard/guard.go) | `TestLinesItCannotParseAreErrors`, `TestPartsItCannotReadAreReported`, `TestEvaluateFailsClosedOnACommandItCannotParse`, `TestEvaluateFailsClosedOnAPartItCannotReadStatically` |
| The gate maps changed paths to lanes from a table, and refuses a commit when a lane fails | [table.go](internal/gate/table.go), [gate.go](internal/gate/gate.go), [snapshot.go](internal/gate/snapshot.go), [main.go](cmd/bindery-gate/main.go), [lanes.tsv](lanes.tsv) | `TestRouteSendsAPathToItsLanes`, `TestAFailingLaneRefusesTheCommitAndSaysHow`, `TestAPassingCommitIsSilent`, `TestEachMatchedLaneRunsOnceHoweverManyPathsMatch`, `TestAPathNoLaneClaimsIsRefusedAndNamed`, `TestAMalformedTableIsRefused`, `TestAnUnstagedBreakageDoesNotRefuseACleanStagedChange`, `TestAStagedBreakageWithItsFixUnstagedIsRefused`, `TestASkipWorktreeFileIsInTheCopyTheChecksRunIn`, `TestTheCopyIsRemovedByClose`, and `TestHookBlocksACommitThatBreaksALane`, `TestHookPassesACleanCommitWhileAnUnstagedFileIsBroken` and `TestHookRefusesAStagedBreakageWhoseFixIsUnstaged` through a real git hook |
| The gate refuses an edited test that has no reason line | [rules.go](internal/gate/rules.go) | `TestAnEditedTestWithoutAReasonLineIsRefused`, `TestTheSameEditWithAReasonLinePasses`, `TestANewTestFilePassesWithoutAReasonLine`, `TestADeletedTestFileIsRefusedWithoutAReasonLine`, and `TestHookRefusesAnEditedTestWithoutAReasonLine` through a real git hook |
| The bus delivers between two named sessions, and loses nothing when two senders write at once | [bus.go](internal/bus/bus.go), [main.go](cmd/bindery-bus/main.go) | `TestAMessageToldIsReturnedByTheReceiversWait`, `TestWaitReturnsAMessageThatArrivesAfterItStarted`, `TestWaitTimesOutWhenNothingArrives` and `TestAMessageIsNeverReturnedTwice` for delivery; `TestManySendersToOneInboxLoseNothingAndDoubleNothing`, `TestAReceiverDrainingWhileSendersSendGetsEachMessageOnce` and, with separate processes, `TestManyProcessesTellingOneInboxLoseNothingAndDoubleNothing` for the concurrent senders; `TestABadNameIsRefusedAndNothingIsWritten` and `TestWhoListsEveryNameWithAnInboxAndWhatItDid`; `TestAWaitInOneProcessReturnsAMessageToldByAnother` across two processes; `TestAWriteThatFailsPartWayLeavesTheInboxAsItWas` for a full disk, `TestWhoMarksABrokenInboxAndStillListsTheOthers` and `TestOpenRefusesASymbolicLinkAtTheBusDirectory` |

## The guard runner

`bindery-guard` is one static binary. It reads one tool call as JSON on standard input, in the shape a coding agent's pre-tool-use hook sends, and runs every guard against it.

```json
{"tool_name": "Bash", "tool_input": {"command": "rm -rf /srv/demo/data"}, "cwd": "/srv/demo/app"}
```

| Field | Meaning |
|---|---|
| `tool_name` | Required. `Bash` carries a shell command line, `Read` carries a file path, and any other tool passes without being looked at. |
| `tool_input.command` | Required and non-empty for `Bash`. The shell command line. |
| `tool_input.file_path` | The file a `Read` call opens. |
| `cwd` | Optional. The directory relative paths in the command are resolved against. It defaults to the runner's own working directory. |

Other fields, such as a session identifier, are ignored. The [examples](examples) directory holds three calls, and the tests run them.

| Exit code | Meaning |
|---|---|
| 0 | Every guard is silent. Nothing is printed. |
| 2 | The call is blocked. A guard refused it, or the runner could not do its job, for example because it was started with an argument it does not take. One line goes to standard error: the guard's name and one sentence of reason, or the usage text. |

There is no other non-zero code, because a pre-tool-use hook blocks only on 2 and lets a call through on any other exit. A call that is not one JSON object of the shape above is refused too, as is one whose shell command cannot be read, so a hook that invokes `bindery-guard` fails closed. A refusal looks like this:

```
bindery-guard: recursive-delete: Recursive delete of "/srv/demo/data" is refused, because it is outside the workspace root.
```

### How the shell is read

Shell is parsed with [mvdan.cc/sh](https://github.com/mvdan/sh) in its bash dialect, never with a hand-written tokeniser, and anything it cannot parse is refused with a reason that says the guard could not read the command. That includes syntax only zsh accepts, such as `${(k)map}`.

The guards walk the parsed tree rather than the text, so a command is found wherever it sits:

- after `&&`, `||`, `;` or `&`, and inside `if`, `for`, `case`, functions, subshells and blocks
- inside `$(...)`, backquotes and `<(...)`, and in a pipeline
- inside `bash -c '...'`, `sh -c`, `eval`, a here-document or a here-string given to a shell
- behind `timeout`, `nice`, `ionice`, `env`, `nohup`, `setsid`, `stdbuf`, `sudo`, `doas`, `command`, `exec`, `xargs` and `find -exec`
- in the body of an `alias` or a `trap`, which are read as command strings
- behind any command the scanner does not know, such as `flock`, `watch`, `parallel`, `systemd-run`, `script -c`, `su -c`, `runuser`, `chroot` or `busybox`
- written as `/bin/rm`, `\rm`, `'rm'` or the dollar-quoted `$'rm'` and `$'\x72m'`, since words are expanded the way a shell would

The last two bullets are one rule, not a list of runners. A command that is neither a tool a guard reads nor on a short list of words-only commands (`echo`, `printf`, `ls`, `man` and the like) is read as a possible runner. Its words are searched for the first one that names a tool the guards read, such as `rm`, `find`, `git`, a shell or an interpreter, and the rest of the line is read from there as the command it runs. A word holding spaces or operators is read as a command string. A words-only command ends the search, so `echo rm -rf /` and `flock lock echo rm -rf /` stay silent. The rule errs toward refusing: `ssh host rm -rf /tmp/x` is judged as a local delete.

A part of the line that depends on the running shell is not guessed at. A command name, a `-c` string, an `eval` text or a delete target built from a variable or a command substitution is treated as unreadable, and so is a shell that takes its script from a pipe, including `bash -`, `bash /dev/stdin` and `bash /dev/fd/0`. Only `$HOME` and a leading `~` are resolved, from the configured home directory.

A word that is unknown where an option could stand might be the flag that matters, so it is not skipped. In `rm $OPTS /srv/x` the unknown word may be `-rf`, and in `git push $F origin main` it may be `--force`; the guards judge those lines as if the flag were there. An unquoted unknown word can also split into a flag and its operand at run time, so `rm $X` (with `X` holding `-rf /`) is read as a possible recursive delete, and so is the input of `xargs rm`. A quoted word such as `rm "$file"` stays one word and is not refused. A shell given an unquoted unknown word, as in `bash $X`, is unreadable, because the word may bring `-c` and a command with it. Downstream of a download, a shell or interpreter with an unknown operand is read as possibly reading the download.

### The guards

| Guard | Refuses | Silent on |
|---|---|---|
| `recursive-delete` | `rm -r` and `find -delete` aimed at `/`, the home directory, a workspace root itself, anything outside the workspace roots, or a target it cannot resolve | the same command inside a workspace root, and `rm` without `-r` |
| `force-push` | `git push` with `--force`, `--force-with-lease`, a `+refspec` or a delete, aimed at a protected branch, and a forced push that names no branch | forced pushes to other branches, and plain pushes to a protected one |
| `pipe-to-shell` | `curl` or `wget` piped into a shell or another interpreter, or handed to one as `-c "$(...)"` or `<(...)` | downloads piped into `jq`, `tee` or `grep`, and interpreters given a script file |
| `secrets-read` | a `Read` call, or a command such as `cat` or `cp`, on a file whose name matches a secrets pattern | template files such as `.env.example`, listings, and paths known only at run time |

The first refusal wins. The order is the one in the table.

### Configuration

Every value has a default and is overridden by an environment variable. Nothing is read from a file, and no default names a path on any machine.

| Variable | Meaning | Default |
|---|---|---|
| `BINDERY_WORKSPACE_ROOTS` | Directories a recursive delete may reach into, separated by `:` | the runner's working directory |
| `BINDERY_PROTECTED_BRANCHES` | Branch names or patterns, separated by `,` | `main,master` |
| `BINDERY_SECRET_PATTERNS` | File and directory name patterns, separated by `,` | `.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa`, `id_ecdsa`, `id_ed25519`, `.ssh`, `.gnupg` |
| `HOME` | The home directory, which `recursive-delete` protects and which `$HOME` and `~` stand for | none, so both are unreadable |

### Limits

The guards read the command line and nothing else: they do not follow symlinks, expand globs against the disk, know aliases or functions defined outside the command line, or see what a script file will do. The wrappers that are looked through by name are a fixed list; any other command is read by the runner rule above, which depends on the runner's command appearing among its words. `secrets-read` is a tripwire for the obvious read and not a boundary. A forced push that does not name its branch is refused, because the current branch is not visible from the command line, so say which branch you mean.

## The commit gate

`bindery-gate` sends the paths a commit changes to lanes, runs each matched lane's check, and refuses the commit when one fails. It is a small gate: the table this repository ships holds four lanes, and the idea is the part to look at, not the count. It uses the standard library only and calls `git` as a subprocess.

It does three things:

1. **It reads the staged change** with `git diff --cached`, as paths with a status: added, modified, deleted, renamed or type-changed. Run from a commit hook, git points that command at the commit's own index, so a commit with a pathspec (`git commit -- notes.md`) is judged on `notes.md` and not on everything staged.
2. **It sends each path to its lanes** from a table and runs each matched lane's check once, however many paths matched it. A deleted path is routed too, and a renamed file is routed by both its old and new path. **A check runs on what is being committed, not on the working tree.** Before the first check the gate writes the index into a fresh temporary directory with `git checkout-index --all`, runs every check there, and removes the directory on every way out, a failing check and a panic included. A broken file that is not staged cannot refuse a clean commit, and a broken staged file cannot pass because its fix was left unstaged. Nothing is written when no lane matches.
3. **It applies one rule across lanes.** A commit that modifies, deletes or renames away an existing test file must carry a line `Test changed: <why>` in its message. A new test file needs no line. The reason is the point: a test whose expectation moves without a reason is how a regression gets a quiet pass, and the line puts the reason in the history next to the change.

### The lanes table

A tab-separated text file, read from `lanes.tsv` at the repository root unless `BINDERY_GATE_TABLE` names another path (relative to the root, or absolute). One row per line, three columns:

```
# lane	pattern	check
go	*.go	make check
go	go.mod	make check
docs	*.md	git diff --cached --check
```

| Column | Meaning |
|---|---|
| lane | A name in lower case letters, digits and hyphens. Rows that share a name are one lane with several patterns, and they must give it the same check. |
| pattern | Which paths the lane claims. A pattern ending in `/` claims everything below that directory. A pattern with no `/` is matched against the file name at any depth. Any other pattern is matched against the whole path from the repository root, and `*` does not cross a `/`. Matching is Go's `path.Match`. |
| check | The command that must exit 0. It runs at the root of the temporary copy of the staged files. Git commands in it, such as `git diff --cached --check`, are pointed at the real repository and the commit's own index. |

Blank lines and lines that start with `#` are skipped. [lanes.tsv](lanes.tsv) is the table for this repository, and a test keeps it readable.

**A check is run without a shell.** It is split on whitespace and run as `argv`, so what the table says is what runs: there is no quoting to get wrong and no row that turns into a script. A row that contains shell syntax (quotes, `$`, `|`, `&`, `;`, `<`, `>`, parentheses, braces, `*`, `?` or a backslash) or starts with an assignment such as `CGO_ENABLED=0` is refused, because without a shell it would run as plain words and not do what it looks like. If a check needs a pipeline, put the pipeline in a script and name the script.

**A path no lane claims is refused.** The alternative, listing it as ungated and letting the commit through, would make every new kind of file a quiet hole in the gate. The refusal names the paths and the fix is a row in the table. **A table that cannot be read is refused whole**, with the line and the reason, because a lane that silently went missing leaves its paths unchecked.

### Running it

As a git `commit-msg` hook, which git hands the message file's path:

```bash
go build -o bin/bindery-gate ./cmd/bindery-gate
cp examples/commit-msg .git/hooks/commit-msg && chmod +x .git/hooks/commit-msg
export BINDERY_GATE="$PWD/bin/bindery-gate"   # or put bindery-gate on PATH
```

The [example hook](examples/commit-msg) is two lines of shell around `exec bindery-gate "$1"`. A commit then goes through the gate every time, and the `Test changed:` line is read from the message you just wrote.

By hand, against what is staged:

```bash
bin/bindery-gate                   # lanes and the unclaimed rule; no message, so an edited test is refused
bin/bindery-gate message.txt       # the same, with the message in a file
```

| Exit code | Meaning |
|---|---|
| 0 | The commit may go ahead. Nothing is printed. |
| 1 | The gate refused it. One line per failing lane or rule goes to standard error, naming it and saying why. |
| 2 | The gate could not do its job: the table is malformed or missing, the directory is not a repository, the message file cannot be read, or it was started with an argument it does not take. One line says which. |

Git blocks a commit on any non-zero exit from a hook, so a gate that fails to run never lets a commit through. A refusal looks like this:

```
bindery-gate: unclaimed: No lane claims "data/rows.csv", so the commit is refused until the table covers it.
bindery-gate: test-changed: The existing test "widget_test.go" is changed, and the commit message has no "Test changed: <why>" line.
bindery-gate: go: The check "make check" exited with status 2, saying: FAIL.
```

The lines come in a fixed order: unclaimed paths, the test rule, then the failing lanes in table order. Every failing lane is reported, not only the first.

### What counts as a test file

By name: a file ending `_test.go` or `_test.py`, starting `test_`, or holding `.test.` or `.spec.` before its extension, and any file below a directory named `test`, `tests` or `__tests__`. The rule is a few lines in [rules.go](internal/gate/rules.go).

### Limits

The gate is a client-side check and is only as strong as its installation: a clone without the hook, or `git commit --no-verify`, skips it, which is why CI runs `make check` as well. The copy holds only tracked files, so a check that needs an untracked or ignored file, such as a generated one, fails in the gate; stage the file or generate it inside the check. The copy holds every file the index holds, including those a sparse checkout leaves out of the work tree. A copy of a large repository takes time, though only on commits that match a lane. A process killed outright, with `kill -9` for one, cannot remove the copy. A check has no timeout and checks run one after another. The test rule reads file names and nothing else: a test kept in a file that does not look like one is not seen, and it cannot tell a good reason from a bad one, only that a reason was written. A copied file is reported by git as an added file, and a mode change alone is a modification.

## The message bus

`bindery-bus` lets named sessions on one machine send each other messages. There is no server: the bus is a directory with one inbox file per name, and every change to an inbox happens under an exclusive `flock` on it. It uses the standard library only.

| Command | Does |
|---|---|
| `bindery-bus tell FROM TO TEXT...` | Appends one message to TO's inbox. The words of the text are joined with single spaces. |
| `bindery-bus wait [--timeout DURATION] NAME` | Blocks until NAME's inbox holds an unread message, prints the oldest one as `#SEQ FROM: TEXT`, and marks it read. The default timeout is `30s`, `0s` looks once and does not wait, and the flag goes before the name. |
| `bindery-bus who` | Lists every name that has an inbox, with its unread count and when it last told and last waited. A name whose files cannot be read is listed as `broken: line 2 is not a message` (or `broken: NAME.state is not readable`), and the other names are listed as usual. |

```bash
bin/bindery-bus tell juniper cobalt "the build is green, ship it"
bin/bindery-bus wait --timeout 10s cobalt     # prints: #1 juniper: the build is green, ship it
```

### Storage

| Variable | Meaning | Default |
|---|---|---|
| `BINDERY_BUS_DIR` | The bus directory, made with mode `0700` if it is missing | `bindery-bus-<uid>` inside the system temporary directory |

For each name there are two files in that directory, both mode `0600`:

- `NAME.jsonl` is the inbox. One message is one JSON line with its sequence number, the time, the sender, the receiver and the text, and the sequence numbers count up from 1 in each inbox.
- `NAME.state` is a small JSON file holding the sequence number of the last message a wait returned, and the times the name last told and last waited. It is replaced whole through a temporary file and a rename.

```
{"seq":1,"time":"2030-01-02T03:04:05.678Z","from":"juniper","to":"cobalt","text":"the build is green, ship it"}
```

**The lock is what keeps senders from losing each other's messages.** To append, a sender opens the inbox, takes `flock(LOCK_EX)`, reads the last sequence number, writes one whole line and closes the file, which releases the lock. If the write fails or is cut short, the sender truncates the inbox back to the size it had before, still under the lock, and reports the failure. Two senders cannot read the same last number, so no sequence number is used twice and no line is cut in half by another. A wait takes the same lock to find the oldest message past the stored read position and to move that position, which is why two waiters on one name each get different messages and a message is never returned twice. The sender's own record is updated after the receiver's inbox is released, never while it is held, so two sessions telling each other cannot wait on each other's locks.

**A name is a file name, so it is checked first.** A name is 1 to 32 lower case letters, digits, hyphens or underscores, starting with a letter or a digit. `..`, `a/b`, an empty name, a dot, an upper case letter and a leading hyphen are refused before any file is touched. A message is refused when it is empty, not valid UTF-8, or longer than 64 KiB.

**The directory is checked too.** Anyone who can write to it can send as any name, so a directory that another user owns, or that group or others can reach, is refused with a message naming the mode. A symbolic link at the bus directory is refused, not followed, because the checks would be made on its target and the link can be pointed elsewhere later; a link higher up the path is only a way of reaching the directory and is fine.

| Exit code | Meaning |
|---|---|
| 0 | The command did its job. A `wait` that printed a message exits 0. |
| 1 | A `wait` timed out with nothing to return. One line goes to standard error, and nothing to standard output. |
| 2 | The command could not do its job: a name is not valid, the message is refused, the directory cannot be used, an inbox holds a line that is not a message, or the arguments are wrong, in which case the usage goes to standard error. |

### Limits

- **POSIX only.** `flock` and the ownership check are Linux and macOS system calls, and the package does not build on Windows.
- **A wait polls.** It looks at its inbox every 50 milliseconds and does not get a notification, so a message can take that long to be seen and an idle wait costs a lock and a read twenty times a second.
- **Delivery is at most once.** A message is marked read before the waiting command prints it, so a wait that dies in between loses that message and does not return it twice.
- **Inboxes are never pruned**, and every command reads the whole inbox it works on, so cost grows with the number of messages kept. Delete a name's two files to clear it.
- **A line that is not a message refuses the inbox**, with its file and line number, rather than being skipped. `tell` and `wait` on that name exit 2, and `who` lists it as `broken` and carries on with the others. A failed append, such as one on a full disk, is cut back to the size the inbox had, so the bus does not make such a line itself; one can still come from a process killed outright in the middle of a write, or from an edit by hand. To recover, when no session is using that name, open `NAME.jsonl`, delete the line the error names (a cut-short append is the last line), and save. The messages before it are untouched and the next message continues their numbering. A `NAME.state` that cannot be read is recovered by deleting that file, after which every message still in the inbox counts as unread.
- **The names are not authenticated.** `FROM` is whatever the sender typed, and any process of the same user can send as any name. The bus is a courtesy between cooperating sessions on one machine, not a boundary between them.
- **One machine.** `flock` is advisory and not reliable over a network file system, so keep the directory on a local disk. Times are the local clock of whoever wrote them.

## Run it

You need Go 1.27 or newer, `make` and `git`. The first build downloads one module, `mvdan.cc/sh/v3`, and after that nothing needs the network.

```bash
make check   # gofmt (any difference fails), go vet ./..., go test -race ./...
make demo    # builds bin/bindery-guard, bin/bindery-gate and bin/bindery-bus and shows all three on invented data
```

`make demo` shows, in order:

1. a destructive command refused by a guard, with its reason
2. a harmless command that the same guard lets through in silence
3. the commit gate in a throwaway repository under a temporary directory: a commit that breaks a lane is blocked, an edited test with no reason line is refused, and the same edit with the line goes through
4. two invented sessions on a bus in a temporary directory: `cobalt` waits in the background, `juniper` tells it a message, and the wait prints it. A second wait finds nothing, and `who` lists both names

Both temporary directories are removed when the demo ends, and nothing in it needs the network.

[docs/from-nothing.md](docs/from-nothing.md) walks through it from a fresh clone.

## What this is not

Bindery is a smaller re-implementation of a private internal system, written fresh for this repository. It is never a copy of that system, and the private one does more. It has, among other things:

- a persistent store behind its sessions. The bus here is a directory of inbox files and nothing more: no database, no history beyond the files, and no search
- many more guards, and many more gate lanes than the table here
- persona and memory handling for the sessions
- chat and forge bridges that carry messages in and out. The bus here has no poller for a chat service or a code host, and nothing outside the machine can reach it
- quiet tells and other delivery modes. Every `tell` here is the same kind of message, and a `wait` returns it

The showcase keeps one small, honest version of three ideas from it, and nothing more. Where this README says what the showcase does, it means this code, and it makes no claim about what the private system does.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md). Changes are listed in [CHANGELOG.md](CHANGELOG.md).

## Licence

MIT, see [LICENSE](LICENSE).
