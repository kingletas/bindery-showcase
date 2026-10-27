# bindery

[![CI](https://github.com/kingletas/bindery/actions/workflows/ci.yml/badge.svg)](https://github.com/kingletas/bindery/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Bindery is a small, runnable showcase of a multi-session AI coding harness: guards that read every tool call before it runs, a commit gate that maps changed paths to lanes, and a message bus between named sessions. It is written in Go, on the standard library plus one shell parser, and every name, path and host in it is invented.

> **Status: the guards are built.** The commit gate and the message bus are the next pieces, so their rows below say "not built yet" until their code and tests land.

## What it claims, and where to check

Each row is a claim about this showcase, not about any other system. A row gets a file and a test when the code exists, and not before.

| Claim | File | Test |
|---|---|---|
| A guard refuses a destructive command and says why | [recursive_delete.go](internal/guard/recursive_delete.go), [force_push.go](internal/guard/force_push.go), [pipe_to_shell.go](internal/guard/pipe_to_shell.go), [secrets_read.go](internal/guard/secrets_read.go) | `TestRecursiveDeleteRefusesAProtectedOrOutsidePath`, `TestForcePushRefusesAProtectedBranch`, `TestPipeToShellRefusesADownloadThatIsRun`, `TestSecretsReadRefusesAConfiguredSecretsPath`, and `TestARefusedCallExitsTwoAndNamesTheGuardOnStderr` for the runner |
| The same guard is silent on a harmless command | the same four files | `TestRecursiveDeleteIsSilentOnHarmlessCommands`, `TestForcePushIsSilentOnHarmlessCommands`, `TestPipeToShellIsSilentOnHarmlessCommands`, `TestSecretsReadIsSilentOnHarmlessCommands`, and `TestASilentCallExitsZeroAndPrintsNothing` for the runner |
| A guard fails closed on a line it cannot parse | [shellscan.go](internal/shellscan/shellscan.go), [guard.go](internal/guard/guard.go) | `TestLinesItCannotParseAreErrors`, `TestPartsItCannotReadAreReported`, `TestEvaluateFailsClosedOnACommandItCannotParse`, `TestEvaluateFailsClosedOnAPartItCannotReadStatically` |
| The gate maps changed paths to lanes, and refuses an edited test that has no reason line | not built yet | not built yet |
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

## Run it

You need Go 1.27 or newer and `make`. The first build downloads one module, `mvdan.cc/sh/v3`, and after that nothing needs the network.

```bash
make check   # gofmt (any difference fails), go vet ./..., go test -race ./...
make demo    # builds bin/bindery-guard and pipes two invented calls through it
```

`make demo` shows one call refused with its reason and one passing in silence. The commit gate and the message bus join it in the later pieces.

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
