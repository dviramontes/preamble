set shell := ["bash", "-euo", "pipefail", "-c"]

bin:
	mkdir -p "$HOME/go/bin"
	mkdir -p ./bin
	go build -o ./bin/pre ./cmd/pre
	ln -sfn "$(pwd)/bin/pre" "$HOME/go/bin/pre"
	ls -l "$HOME/go/bin/pre"
