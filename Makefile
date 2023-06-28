GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
MODULE_NAME ?= $(shell cat go.mod | head -n1 | cut -f 2 -d ' ')

.PHONY: mod
mod:
	go mod download

.PHONY: start
start: mod
	sh scripts/start.sh

# TODO: (shinta) it will disappear once flake is supported
.PHONY: store
store:
	go build -o bin/runetale cmd/runetale/main.go
	nix-store --add bin/runetale