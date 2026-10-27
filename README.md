# bindery

[![CI](https://github.com/kingletas/bindery/actions/workflows/ci.yml/badge.svg)](https://github.com/kingletas/bindery/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Bindery is a small, runnable showcase of a multi-session AI coding harness: guards that read every tool call before it runs, a commit gate that maps changed paths to lanes, and a message bus between named sessions. It is written in Go, on the standard library plus one shell parser, and every name, path and host in it is invented.

> **Status: the guards and the commit gate are built.** The message bus is the next piece, so its row below says "not built yet" until its code and tests land.

## What it claims, and where to check

Each row is a claim about this showcase, not about any other system. A row gets a file and a test when the code exists, and not before.

| Claim | File | Test |
|---|---|---|
| A guard refuses a destructive command and says why | [recursive_delete.go](internal/guard/recursive_delete.go), [force_push.go](internal/guard/force_push.go), [pipe_to_shell.go](internal/guard/pipe_to_shell.go), [secrets_read.go](internal/guard/secrets_read.go) | `TestRecursiveDeleteRefusesAProtectedOrOutsidePath`, `TestForcePushRefusesAProtectedBranch`, `TestPipeToShellRefusesADownloadThatIsRun`, `TestSecretsReadRefusesAConfiguredSecretsPath`, and `TestARefusedCallExitsTwoAndNamesTheGuardOnStderr` for the runner |
| The same guard is silent on a harmless command | the same four files | `TestRecursiveDeleteIsSilentOnHarmlessCommands`, `TestForcePushIsSilentOnHarmlessCommands`, `TestPipeToShellIsSilentOnHarmlessCommands`, `TestSecretsReadIsSilentOnHarmlessCommands`, and `TestASilentCallExitsZeroAndPrintsNothing` for the runner |
| A guard fails closed on a line it cannot parse | [shellscan.go](internal/shellscan/shellscan.go), [guard.go](internal/guard/guard.go) | `TestLinesItCannotParseAreErrors`, `TestPartsItCannotReadAreReported`, `TestEvaluateFailsClosedOnACommandItCannotParse`, `TestEvaluateFailsClosedOnAPartItCannotReadStatically` |
| The gate maps changed paths to lanes from a table, and refuses a commit when a lane fails | [table.go](internal/gate/table.go), [gate.go](internal/gate/gate.go), [main.go](cmd/bindery-gate/main.go), [lanes.tsv](lanes.tsv) | `TestRouteSendsAPathToItsLanes`, `TestAFailingLaneRefusesTheCommitAndSaysHow`, `TestAPassingCommitIsSilent`, `TestEachMatchedLaneRunsOnceHoweverManyPathsMatch`, `TestAPathNoLaneClaimsIsRefusedAndNamed`, `TestAMalformedTableIsRefused`, and `TestHookBlocksACommitThatBreaksALane` through a real git hook |
| The gate refuses an edited test that has no reason line | [rules.go](internal/gate/rules.go) | `TestAnEditedTestWithoutAReasonLineIsRefused`, `TestTheSameEditWithAReasonLinePasses`, `TestANewTestFilePassesWithoutAReasonLine`, `TestADeletedTestFileIsRefusedWithoutAReasonLine`, and `TestHookRefusesAnEditedTestWithoutAReasonLine` through a real git hook |
| The bus delivers between two named sessions, and loses nothing when two senders write at once | not built yet | not built yet |

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
2. **It sends each path to its lanes** from a table and runs each matched lane's check once, however many paths matched it. A deleted path is routed too, and a renamed file is routed by both its old and new path.
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
| check | The command that must exit 0, run from the repository root. |

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

The gate is a client-side check and is only as strong as its installation: a clone without the hook, or `git commit --no-verify`, skips it, which is why CI runs `make check` as well. Checks run in the working tree, not on the staged content, so a file edited after `git add` is the one that gets checked. A check has no timeout and checks run one after another. The test rule reads file names and nothing else: a test kept in a file that does not look like one is not seen, and it cannot tell a good reason from a bad one, only that a reason was written. A copied file is reported by git as an added file, and a mode change alone is a modification.

## Run it

You need Go 1.27 or newer, `make` and `git`. The first build downloads one module, `mvdan.cc/sh/v3`, and after that nothing needs the network.

```bash
make check   # gofmt (any difference fails), go vet ./..., go test -race ./...
make demo    # builds bin/bindery-guard and bin/bindery-gate and shows both on invented data
```

`make demo` shows one call refused with its reason and one passing in silence, then the commit gate in a throwaway repository under a temporary directory that the demo removes: a commit that breaks a lane is blocked, an edited test with no reason line is refused, and the same edit with the line goes through. The message bus joins it in the last piece.

[docs/from-nothing.md](docs/from-nothing.md) walks through it from a fresh clone.

## What this is not

Bindery is a smaller re-implementation of a private internal system, written fresh for this repository. It is never a copy of that system, and the private one does more. It has, among other things:

- a persistent store behind its sessions
- many more guards, and many more gate lanes than the table here
- persona and memory handling for the sessions
- chat and forge bridges that carry messages in and out

The showcase keeps one small, honest version of three ideas from it, and nothing more. Where this README says what the showcase does, it means this code, and it makes no claim about what the private system does.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md). Changes are listed in [CHANGELOG.md](CHANGELOG.md).

## Licence

MIT, see [LICENSE](LICENSE).
