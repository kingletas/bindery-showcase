# From nothing to a working bindery

By the end of this you will have cloned the repository, run its check and know what is built and what is not yet.

## Contents

- [What this is](#what-this-is)
- [Step 1: install it](#step-1-install-it)
- [Step 2: run the check](#step-2-run-the-check)
- [Step 3: run the demo](#step-3-run-the-demo)
- [Where to go next](#where-to-go-next)

## What this is

Bindery is a small, runnable showcase of a multi-session AI coding harness. When several AI sessions share one machine, something has to read each tool call before it runs, decide which kind of change a commit is, and let the sessions talk to each other without losing a message. This repository shows each of those three jobs in plain Go on invented data.

It is built in pieces, and the README says which pieces exist.

## Step 1: install it

You need Go 1.27 or newer, `make` and `git`. The module has one dependency, the shell parser `mvdan.cc/sh/v3`, which Go downloads on the first build.

```bash
git clone https://github.com/kingletas/bindery && cd bindery
```

## Step 2: run the check

```bash
make check
```

It checks formatting, vets and tests the whole module with the race detector. A healthy run prints `go vet` and `go test` output (one `ok` line per package). The format step is silent when every file is clean and lists the files that differ when not.

## Step 3: run the demo

```bash
make demo
```

This builds `bin/bindery-guard` and `bin/bindery-gate`. It pipes two invented tool calls through the guard. The first, a recursive delete outside the workspace root, is refused: you see the guard's name, one sentence of reason and `exit 2`. The second, a recursive delete inside the root, passes in silence with `exit 0`.

Then it makes a throwaway git repository under a temporary directory, installs the gate as its `commit-msg` hook, and tries four commits. One passes in silence. One changes a file a lane watches so that its check fails, and is blocked with the lane's name. One edits an existing test and is refused because the message has no `Test changed:` line. The last makes that same edit with the line and goes through. The directory is removed when the demo ends. When the message bus lands, this is the command that shows it too.

## Where to go next

- [README](../README.md), which lists every claim and the file and test that will prove it
- [CONTRIBUTING.md](../CONTRIBUTING.md)
