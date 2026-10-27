# Changelog

## Unreleased

- The repository exists: a Go module, a `make check` that fails on any gofmt difference and runs `go vet ./...` and `go test -race ./...`, and a CI workflow that runs that same target.
- `bindery-guard` reads one tool call as JSON on standard input and runs four guards against it. It exits 0 and prints nothing when every guard is silent, and exits 2 with the guard's name and one sentence of reason on standard error when one refuses. A call that is not valid JSON of the expected shape is refused too.
- Shell is read with `mvdan.cc/sh/v3/syntax`, the module's one dependency, and walked as a tree. A destructive command is found after `&&`, inside `$(...)`, in a pipeline, inside `bash -c`, and behind wrappers such as `timeout`, `nice`, `env`, `sudo` and `xargs`, including when the command name is written `$'rm'`. A line the parser rejects, which includes syntax only zsh accepts, is refused with a reason that says the command could not be read, and so is a part of the line that is only known at run time.
- The `recursive-delete` guard refuses `rm -r` and `find -delete` on `/`, the home directory, a workspace root itself or anything outside the workspace roots. The `force-push` guard refuses a forced push or a delete on a protected branch. The `pipe-to-shell` guard refuses a download that is run as it arrives. The `secrets-read` guard refuses a read of a file that matches a secrets pattern. Roots, branches and patterns come from `BINDERY_WORKSPACE_ROOTS`, `BINDERY_PROTECTED_BRANCHES` and `BINDERY_SECRET_PATTERNS`, with defaults that name no real path.
- `make demo` builds the runner and pipes two invented calls through it, one refused and one silent, and fails if either exit code is wrong.
- The README table points the first three claims at their files and tests, and documents the call's JSON shape, the exit codes and the configuration. The commit gate and the message bus are still marked "not built yet".
