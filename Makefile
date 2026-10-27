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

.PHONY: build
build: ## Build the guard runner, the commit gate and the message bus into bin/
	go build -o bin/bindery-guard ./cmd/bindery-guard
	go build -o bin/bindery-gate ./cmd/bindery-gate
	go build -o bin/bindery-bus ./cmd/bindery-bus

# The demo pipes two invented tool calls through the guard runner on an
# invented machine (a home directory and one workspace root that exist
# nowhere), runs the commit gate in a throwaway repository, then lets two
# invented sessions exchange a message on a bus in a temporary directory. Both
# temporary directories are removed. It needs no network and reads no real
# path, and it fails if any step gets the wrong exit code.
DEMO_ENV := HOME=/home/demo BINDERY_WORKSPACE_ROOTS=/srv/demo/app

.PHONY: demo
demo: build ## Show the guard runner, the commit gate and the message bus on invented data
	@echo "== A destructive command, outside the workspace root (expect a refusal)"
	@cat examples/refused.json
	@code=0; env $(DEMO_ENV) bin/bindery-guard < examples/refused.json || code=$$?; \
	echo "exit $$code"; test "$$code" -eq 2
	@echo
	@echo "== A harmless command, inside the workspace root (expect silence)"
	@cat examples/silent.json
	@code=0; env $(DEMO_ENV) bin/bindery-guard < examples/silent.json || code=$$?; \
	echo "exit $$code"; test "$$code" -eq 0
	@echo
	@echo "== The commit gate, in a throwaway repository that is removed afterwards"
	@sh examples/gate/demo.sh bin
	@echo
	@echo "== The message bus, between two sessions on a bus directory that is removed afterwards"
	@sh examples/bus/demo.sh bin
