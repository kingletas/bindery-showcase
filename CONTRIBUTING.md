# Contributing

Thanks for looking.

## The check

```bash
make check
```

That's everything a commit has to pass: gofmt (any difference fails), `go vet ./...` and `go test -race ./...`. CI runs exactly this target and nothing else.

You need Go 1.27 or newer and nothing else. The module depends on `mvdan.cc/sh/v3` alone and otherwise uses the standard library. The first build downloads that one module, and after that the tests need no network and no credentials.

## What a change should look like

- One concern per pull request, with the reasoning in the description.
- `make check` green.
- A test that fails before your change and passes after it. **One direction
  isn't a test**: something that fires isn't evidence it can be quiet, and
  something quiet isn't evidence it can fire.
- An entry in `CHANGELOG.md` under a new heading, saying what changed for
  somebody using this rather than what the diff did.
- Comments say what the code does or what it guards against, in a sentence or
  two. History belongs in the commit message and the changelog.

## Security

Don't open a public issue for a vulnerability.
[SECURITY.md](SECURITY.md) has the reporting route.
