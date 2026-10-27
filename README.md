# bindery

[![CI](https://github.com/kingletas/bindery/actions/workflows/ci.yml/badge.svg)](https://github.com/kingletas/bindery/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Bindery is a small, runnable showcase of a multi-session AI coding harness: guards that read every tool call before it runs, a commit gate that maps changed paths to lanes, and a message bus between named sessions. It is written in Go, on the standard library alone for now, and every name, path and host in it is invented.

> **Status: scaffold.** The repository, its check and its CI exist. The guards, the commit gate and the message bus are the next pieces, so every row below says "not built yet" until its code and test land.

## What it claims, and where to check

Each row is a claim about this showcase, not about any other system. A row gets a file and a test when the code exists, and not before.

| Claim | File | Test |
|---|---|---|
| A guard refuses a destructive command and says why | not built yet | not built yet |
| The same guard is silent on a harmless command | not built yet | not built yet |
| A guard fails closed on a line it cannot parse | not built yet | not built yet |
| The gate maps changed paths to lanes, and refuses an edited test that has no reason line | not built yet | not built yet |
| The bus delivers between two named sessions, and loses nothing when two senders write at once | not built yet | not built yet |

## Run it

You need Go 1.27 or newer and `make`. There are no dependencies to download.

```bash
make check   # gofmt (any difference fails), go vet ./..., go test -race ./...
make demo    # a placeholder until the later pieces land
```

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
