.PHONY: build test run deploy

build:
	go build -o bin/cooked ./cmd/cooked

test:
	go vet ./...
	go test -race ./...

run: build
	./bin/cooked -addr 127.0.0.1:8095 -db cooked.db

deploy:
	fly deploy
