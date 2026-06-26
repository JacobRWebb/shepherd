# Shepherd Makefile

MODULE  := github.com/JacobRWebb/shepherd
PKG     := $(MODULE)/internal/cli
BIN     := shepherd
BINDIR  := bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X $(PKG).version=$(VERSION) -X $(PKG).commit=$(COMMIT) -X $(PKG).date=$(DATE)

.PHONY: build install test vet lint fmt tidy run clean cross

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINDIR)/$(BIN) ./cmd/shepherd

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/shepherd

test:
	go test ./...

vet:
	go vet ./...

lint:
	staticcheck ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

run: build
	$(BINDIR)/$(BIN) $(ARGS)

# Sanity-check the unix build-tagged files from any host.
cross:
	GOOS=linux  GOARCH=amd64 go build ./...
	GOOS=darwin GOARCH=arm64 go build ./...

clean:
	rm -rf $(BINDIR)
