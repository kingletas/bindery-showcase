# Changelog

## Unreleased

- The repository exists: a Go module on the standard library alone, a `make check` that fails on any gofmt difference and runs `go vet ./...` and `go test -race ./...`, and a CI workflow that runs that same target.
- `make demo` is a placeholder that says the demo arrives with the guards, the commit gate and the message bus.
- The README lists the five claims the showcase will make, each marked "not built yet" until its code and test land.
