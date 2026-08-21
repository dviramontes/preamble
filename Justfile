set shell := ["bash", "-euo", "pipefail", "-c"]

default:
	@just --list

check:
	test -z "$(gofmt -l .)"
	go vet ./...
	go test ./...
	go build -o ./bin/pre ./cmd/pre

test:
	go test ./...

bin:
	mkdir -p "$HOME/go/bin"
	mkdir -p ./bin
	go build -o ./bin/pre ./cmd/pre
	ln -sfn "$(pwd)/bin/pre" "$HOME/go/bin/pre"
	ls -l "$HOME/go/bin/pre"
