# Every verb is a make target, and `make help` lists them. CI runs `make check`
# and nothing else, so a green check here predicts a green run there.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

.PHONY: check
check: fmt-check vet test ## Everything a commit has to pass

# gofmt -l prints the files whose formatting differs and exits 0 either way, so
# the recipe fails on any output. Only this module's packages are listed, which
# keeps a nested worktree out of it.
.PHONY: fmt-check
fmt-check: ## Fail when any file is not gofmt-clean
	@unformatted="$$(gofmt -l $$(go list -f '{{.Dir}}' ./...))"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt would change:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet over every package
	go vet ./...

.PHONY: test
test: ## Run the tests with the race detector
	go test -race ./...

.PHONY: demo
demo: ## Placeholder: the demo arrives with the later pieces
	@echo "The demo arrives with the guards, the commit gate and the message bus."
	@echo "Nothing is built yet beyond the scaffold; see the README table."
