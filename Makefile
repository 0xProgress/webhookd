.PHONY: build test lint fmt vet clean release

# VERSION strips the leading "v" from a git tag so that a tag of
# "v0.1.0" produces the string "0.1.0". This matches what GoReleaser's
# {{.Version}} produces at release time, so `webhookd --version` reports
# the same string regardless of whether the binary was built locally
# with `make build` or downloaded from a release. When HEAD is not on a
# tag, `git describe --tags --always` returns a bare commit hash (no
# leading "v"), and sed is a no-op.
VERSION := $(shell git describe --tags --always | sed 's/^v//')

build:
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o bin/webhookd .

test:
	go test -race -count=1 ./...

test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w .

vet:
	go vet ./...

check: fmt vet lint test

clean:
	rm -rf bin/ coverage.out coverage.html

release:
	goreleaser release --clean
